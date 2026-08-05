import asyncio
import json
import logging
import os
import re
from datetime import datetime, timezone
from pathlib import Path

from .errors import ScraperError

log = logging.getLogger("instagram-browser")
POST_SELECTOR = 'a[href*="/p/"], a[href*="/reel/"], a[href*="/tv/"]'


async def wait_for_instagram_page_state(page, timeout_ms=30_000) -> str:
    deadline = asyncio.get_running_loop().time() + timeout_ms / 1000
    while asyncio.get_running_loop().time() < deadline:
        url = page.url.lower()
        if any(part in url for part in ("/challenge/", "/checkpoint/")):
            return "challenge_required"
        try:
            if await page.locator(POST_SELECTOR).count(): return "profile_ready"
            if await page.locator('input[name="username"], input[name="password"]').count() or "/accounts/login/" in url:
                return "login_required"
            body = (await page.locator("body").inner_text(timeout=2000)).lower()
            if any(text in body for text in ("sorry, this page isn't available", "bu sayfaya ulaşılamıyor", "sayfa kullanılamıyor")):
                return "profile_not_found"
            if await page.locator("main, header").count():
                if any(text in body for text in ("this account is private", "bu hesap gizli")): return "profile_private"
                return "page_loaded_without_posts"
        except Exception:
            pass
        await asyncio.sleep(0.5)
    return "timeout"


class BrowserPublicProvider:
    name = "browser_public"
    _playwright = None
    _context = None
    _open_lock = asyncio.Lock()

    def __init__(self):
        self.profile_dir = Path(os.path.expandvars(os.path.expanduser(os.getenv("INSTAGRAM_BROWSER_PROFILE_DIR", "~/.config/sosyalmedyatakip/chromium-profile"))))
        self.headless = os.getenv("INSTAGRAM_BROWSER_HEADLESS", "false").lower() == "true"
        self.debug_dir = Path(os.getenv("INSTAGRAM_DEBUG_DIR", str(Path.cwd().parent / ".runtime" / "debug")))

    async def open(self, navigate_home=False):
        async with type(self)._open_lock:
            cls = type(self)
            if cls._context:
                try:
                    if cls._context.pages:
                        if navigate_home: asyncio.create_task(cls._context.pages[0].goto("https://www.instagram.com/", wait_until="domcontentloaded", timeout=90_000))
                        return cls._context
                except Exception:
                    cls._context = cls._playwright = None
            try:
                from playwright.async_api import async_playwright
                self.profile_dir.mkdir(parents=True, exist_ok=True)
                cls._playwright = await async_playwright().start()
                cls._context = await cls._playwright.chromium.launch_persistent_context(
                    str(self.profile_dir), headless=self.headless,
                    viewport={"width": 1440, "height": 1000}, locale="tr-TR", timezone_id="Europe/Istanbul",
                )
                cls._context.set_default_navigation_timeout(90_000)
                cls._context.set_default_timeout(30_000)
                async def route_assets(route):
                    if route.request.resource_type in {"font", "media"}: await route.abort()
                    else: await route.continue_()
                await cls._context.route("**/*", route_assets)
                if navigate_home:
                    page = cls._context.pages[0] if cls._context.pages else await cls._context.new_page()
                    asyncio.create_task(page.goto("https://www.instagram.com/", wait_until="domcontentloaded", timeout=90_000))
                return cls._context
            except Exception as exc:
                raise ScraperError("PROVIDER_UNAVAILABLE", f"Chromium başlatılamadı: {exc}", 503) from exc

    async def status(self):
        context = type(self)._context
        if not context:
            return {"browser_running": False, "instagram_authenticated": False, "current_url": "", "login_required": True, "challenge_required": False}
        try:
            pages = context.pages
            page = pages[0] if pages else None
            current_url = page.url if page else ""
            login_form = bool(page and await page.locator('input[name="username"], input[name="password"]').count())
            shell = bool(page and await page.locator("main, header").count())
            cookies = await context.cookies("https://www.instagram.com")
            session_cookie = any(c.get("name") == "sessionid" and c.get("value") for c in cookies)
            challenge = any(x in current_url.lower() for x in ("/challenge/", "/checkpoint/"))
            return {"browser_running": True, "instagram_authenticated": bool(session_cookie and shell and not login_form and not challenge),
                    "current_url": current_url, "login_required": login_form or "/accounts/login/" in current_url,
                    "challenge_required": challenge}
        except Exception:
            type(self)._context = type(self)._playwright = None
            return {"browser_running": False, "instagram_authenticated": False, "current_url": "", "login_required": True, "challenge_required": False}

    async def _debug(self, page, response, exc):
        self.debug_dir.mkdir(parents=True, exist_ok=True)
        stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
        prefix = self.debug_dir / f"instagram-timeout-{stamp}"
        try: await page.screenshot(path=str(prefix.with_suffix(".png")), full_page=True)
        except Exception: pass
        try: prefix.with_suffix(".html").write_text(await page.content(), encoding="utf-8")
        except Exception: pass
        try:
            safe = {"url":page.url, "title":await page.title(), "ready_state":await page.evaluate("document.readyState"),
                    "body_exists":bool(await page.locator("body").count()), "post_link_count":await page.locator(POST_SELECTOR).count(),
                    "login_form_exists":bool(await page.locator('input[name="username"],input[name="password"]').count()),
                    "challenge_detected":any(x in page.url.lower() for x in ("/challenge/", "/checkpoint/")),
                    "response_status":getattr(response, "status", None), "exception_type":type(exc).__name__ if exc else "", "exception_message":str(exc or "")}
            prefix.with_suffix(".json").write_text(json.dumps(safe, ensure_ascii=False, indent=2), encoding="utf-8")
        except Exception: pass

    async def scrape(self, username, known, max_posts):
        from playwright.async_api import TimeoutError as PlaywrightTimeoutError
        context = await self.open()
        profile_url = f"https://www.instagram.com/{username}/"
        pages = context.pages
        usable = next((p for p in pages if not p.is_closed()), None)
        page = usable or await context.new_page()
        for extra in list(context.pages):
            if extra != page and username in extra.url:
                await extra.close()
        response = goto_error = None
        log.info("[instagram-browser] navigating url=%s", profile_url)
        try:
            response = await page.goto(profile_url, wait_until="domcontentloaded", timeout=90_000)
            log.info("[instagram-browser] domcontentloaded")
        except PlaywrightTimeoutError as exc:
            goto_error = exc
            try:
                ready = await page.evaluate("document.readyState")
                if "instagram.com" in page.url and ready in {"interactive", "complete"} and await page.locator("body").count():
                    log.info("[instagram-browser] goto timed out but DOM is available")
                else:
                    await self._debug(page, response, exc)
            except Exception:
                await self._debug(page, response, exc)
        try:
            for label in ("Allow all cookies", "Tüm çerezlere izin ver", "Only allow essential cookies"):
                button = page.get_by_text(label, exact=True)
                if await button.count(): await button.first.click(timeout=2000); break
        except Exception: pass
        state = await wait_for_instagram_page_state(page)
        log.info("[instagram-browser] page state=%s", state)
        if state == "login_required":
            log.info("[instagram-browser] login required"); raise ScraperError("BROWSER_LOGIN_REQUIRED", "Instagram oturumu gerekiyor. Açılan Chromium penceresinden giriş yapın.", 401)
        if state == "challenge_required":
            log.info("[instagram-browser] challenge required"); raise ScraperError("INSTAGRAM_CHALLENGE_REQUIRED", "Instagram güvenlik doğrulaması gerekiyor. Açılan tarayıcıdaki işlemi tamamlayın.", 403)
        if state == "profile_not_found": raise ScraperError("PROFILE_NOT_FOUND", "Instagram profili bulunamadı", 404)
        if state == "profile_private": raise ScraperError("PROFILE_PRIVATE", "Bu profil gizli olduğu için gönderiler alınamıyor", 403)
        if state == "timeout" and goto_error:
            await self._debug(page, response, goto_error); raise ScraperError("SCRAPE_TIMEOUT", "Instagram sayfasına belirtilen sürede ulaşılamadı. İnternet bağlantısını ve Instagram tarayıcı oturumunu kontrol edin.", 504)
        links, seen, stagnant = [], set(), 0
        for _ in range(10):
            hrefs = await page.locator(POST_SELECTOR).evaluate_all("els => els.map(e => e.href)")
            before = len(links)
            for href in hrefs:
                match = re.search(r"/(?:p|reel|tv)/([^/?#]+)", href)
                if match and match.group(1) not in seen: seen.add(match.group(1)); links.append((href.split("?")[0], match.group(1)))
            log.info("[instagram-browser] post links found=%d", len(links))
            if len(links) >= max_posts: break
            stagnant = stagnant + 1 if len(links) == before else 0
            if stagnant >= 2: break
            await page.evaluate("window.scrollTo(0, document.body.scrollHeight)"); await page.wait_for_timeout(1500)
        if not links: raise ScraperError("NO_PUBLIC_POSTS", "Profil açık ancak erişilebilir gönderi bulunamadı", 404)
        for link, shortcode in links[:max_posts]:
            if shortcode in known: break
            detail = await context.new_page()
            try:
                await detail.goto(link, wait_until="domcontentloaded", timeout=60_000)
                await detail.wait_for_timeout(1000)
                caption = ""
                for selector in ('meta[property="og:description"]', "article", "main"):
                    try:
                        loc = detail.locator(selector).first
                        caption = (await loc.get_attribute("content") if selector.startswith("meta") else await loc.inner_text(timeout=3000)) or ""
                        if caption: break
                    except Exception: pass
                image = await detail.locator('meta[property="og:image"]').get_attribute("content") or ""
                video = await detail.locator('meta[property="og:video"]').get_attribute("content") or ""
                published = await detail.locator("time").first.get_attribute("datetime") if await detail.locator("time").count() else ""
                yield {"external_id":shortcode,"shortcode":shortcode,"username":username,"caption":caption,
                       "published_at":published or datetime.now(timezone.utc).isoformat(),"permalink":link,
                       "media_type":"VIDEO" if video or "/reel/" in link else "IMAGE","thumbnail_url":image,"media_url":video or image,
                       "likes_count":0,"comments_count":0,"is_pinned":False}
            except PlaywrightTimeoutError:
                log.warning("[instagram-browser] post timed out shortcode=%s", shortcode)
            finally: await detail.close()


class InstagrapiPublicProvider:
    name = "instagrapi_public"
    async def scrape(self, username, known, max_posts):
        from instagrapi import Client
        client = Client(); info = await asyncio.to_thread(client.user_info_by_username_gql, username)
        medias = await asyncio.to_thread(client.user_medias_gql, info.pk, max_posts)
        for media in medias:
            if media.code in known: break
            yield {"external_id":str(media.pk),"shortcode":media.code,"username":username,"caption":media.caption_text or "",
                   "published_at":media.taken_at.isoformat(),"permalink":f"https://www.instagram.com/p/{media.code}/","media_type":str(media.media_type),
                   "thumbnail_url":str(media.thumbnail_url or ""),"media_url":str(media.thumbnail_url or ""),"likes_count":media.like_count or 0,
                   "comments_count":media.comment_count or 0,"is_pinned":False}


class InstaloaderSessionProvider:
    name = "instaloader_session"
    async def scrape(self, username, known, max_posts):
        raise ScraperError("PROVIDER_UNAVAILABLE", "Instaloader session provider bu çalıştırmada etkin değil", 503)


def get_provider():
    name = os.getenv("INSTAGRAM_PROVIDER", "browser_public").lower()
    providers = {"browser_public":BrowserPublicProvider,"instagrapi_public":InstagrapiPublicProvider,"instaloader_session":InstaloaderSessionProvider}
    if name not in providers: raise ScraperError("PROVIDER_UNAVAILABLE", f"Bilinmeyen Instagram provider: {name}", 503)
    return providers[name]()
