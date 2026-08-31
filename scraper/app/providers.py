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
        network_posts = {}

        def collect_nodes(value):
            if isinstance(value, dict):
                code = value.get("shortcode") or value.get("code")
                if code and isinstance(code, str) and 3 < len(code) < 32:
                    caption_value = value.get("caption_text") or value.get("caption") or ""
                    if isinstance(caption_value, dict): caption_value = caption_value.get("text", "")
                    timestamp = value.get("taken_at_timestamp") or value.get("taken_at")
                    published = datetime.fromtimestamp(timestamp, timezone.utc).isoformat() if isinstance(timestamp, (int, float)) else ""
                    network_posts[code] = {"external_id":str(value.get("id") or value.get("pk") or code), "shortcode":code,
                        "username":username, "caption":caption_value if isinstance(caption_value,str) else "", "published_at":published,
                        "permalink":f"https://www.instagram.com/p/{code}/", "media_type":str(value.get("product_type") or value.get("media_type") or "IMAGE").upper(),
                        "thumbnail_url":value.get("display_url") or value.get("thumbnail_url") or "", "media_url":value.get("video_url") or value.get("display_url") or value.get("thumbnail_url") or "",
                        "likes_count":value.get("like_count") or 0, "comments_count":value.get("comment_count") or 0, "is_pinned":bool(value.get("is_pinned"))}
                for child in value.values(): collect_nodes(child)
            elif isinstance(value, list):
                for child in value: collect_nodes(child)

        async def capture_response(response):
            url = response.url.lower()
            if not any(token in url for token in ("graphql", "feed/user", "web_profile_info", "polarisprofile")): return
            try: collect_nodes(await response.json())
            except Exception: pass

        def on_response(response): asyncio.create_task(capture_response(response))
        page.on("response", on_response)
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
        links, seen, stagnant, scroll_round = [], set(), 0, 0
        self.discovered_count, self.scroll_round = 0, 0

        # Bu bir gönderi sayısı sınırı değildir. Instagram gerçekten yeni içerik
        # üretmediğinde işlemin sonsuza kadar kilitlenmesini engeller.
        no_new_limit = max(
            20,
            int(os.getenv("INSTAGRAM_NO_NEW_POST_ROUNDS", "60")),
        )
        scroll_wait_ms = max(
            2000,
            int(os.getenv("INSTAGRAM_SCROLL_WAIT_MS", "3000")),
        )

        async def collect_current_links():
            before = len(links)

            try:
                hrefs = await page.locator(POST_SELECTOR).evaluate_all(
                    "els => els.map(e => e.href).filter(Boolean)"
                )
            except Exception:
                hrefs = []

            for href in hrefs:
                match = re.search(r"/(?:p|reel|tv)/([^/?#]+)", href)
                if not match:
                    continue

                shortcode = match.group(1)
                if shortcode in seen:
                    continue

                seen.add(shortcode)
                links.append((href.split("?")[0], shortcode))

            # Network listener tarafından bulunan içerikler DOM sanallaştırılsa
            # bile kaybolmasın.
            for shortcode, post in list(network_posts.items()):
                if shortcode in seen:
                    continue

                seen.add(shortcode)
                links.append((post["permalink"], shortcode))

            self.discovered_count = len(links)
            return len(links) - before

        async def click_load_more():
            labels = (
                "Load more",
                "Daha fazla yükle",
                "See more posts",
                "Daha fazla gönderi gör",
                "Show more",
                "Daha fazlasını göster",
            )

            for label in labels:
                try:
                    button = page.get_by_text(label, exact=True)
                    if await button.count():
                        await button.last.click(timeout=3000)
                        log.info(
                            "[full-sync] load-more clicked label=%s",
                            label,
                        )
                        return True
                except Exception:
                    continue

            return False

        async def force_profile_scroll():
            anchors = page.locator(POST_SELECTOR)

            try:
                count = await anchors.count()
                if count:
                    await anchors.nth(count - 1).scroll_into_view_if_needed(
                        timeout=5000
                    )
            except Exception:
                pass

            # Instagram bazı sürümlerde body yerine ayrı bir scroll alanı
            # kullanıyor. En büyük kaydırılabilir alanı bulup aşağı indir.
            try:
                return await page.evaluate("""
() => {
    const root =
        document.scrollingElement ||
        document.documentElement ||
        document.body;

    const candidates = [
        root,
        ...document.querySelectorAll('main, section, div')
    ];

    let target = root;
    let bestRange = Math.max(
        0,
        root.scrollHeight - root.clientHeight
    );

    for (const element of candidates) {
        if (!element || element === root) continue;

        const style = window.getComputedStyle(element);
        const overflow = style.overflowY || "";
        const range = element.scrollHeight - element.clientHeight;

        if (
            range > bestRange &&
            range > 300 &&
            /auto|scroll/.test(overflow)
        ) {
            target = element;
            bestRange = range;
        }
    }

    if (target === root) {
        window.scrollBy(
            0,
            Math.max(window.innerHeight * 1.5, 1400)
        );
        window.scrollTo(0, root.scrollHeight);
    } else {
        target.scrollBy(
            0,
            Math.max(target.clientHeight * 1.5, 1400)
        );
        target.scrollTop = target.scrollHeight;
    }

    return {
        tag: target.tagName || "ROOT",
        top: target === root ? window.scrollY : target.scrollTop,
        height: target.scrollHeight,
        viewport: target === root
            ? window.innerHeight
            : target.clientHeight
    };
}
""")
            except Exception:
                try:
                    await page.mouse.wheel(0, 3000)
                    await page.keyboard.press("End")
                except Exception:
                    pass
                return {}

        while True:
            scroll_round += 1
            self.scroll_round = scroll_round

            newly_found = await collect_current_links()

            log.info(
                "[full-sync] scroll_round=%d discovered=%d added=%d",
                scroll_round,
                len(links),
                newly_found,
            )

            # Yalnızca normal, sınırlı senkronizasyonda uygulanır.
            # Full history max_posts=0 gönderdiği için burada durmaz.
            if max_posts and len(links) >= max_posts:
                log.info(
                    "[full-sync] requested max_posts reached=%d",
                    max_posts,
                )
                break

            before_scroll = len(links)

            if stagnant:
                await click_load_more()

            scroll_state = await force_profile_scroll()

            # Tek bir sabit sleep yerine birkaç kez DOM ve yakalanan network
            # cevaplarını kontrol et.
            wait_steps = max(4, scroll_wait_ms // 500)

            for _ in range(wait_steps):
                await page.wait_for_timeout(500)
                await collect_current_links()

                if len(links) > before_scroll:
                    break

            if len(links) > before_scroll:
                stagnant = 0
            else:
                stagnant += 1

                log.info(
                    "[full-sync] no_new_posts round=%d/%d "
                    "discovered=%d scroll=%s",
                    stagnant,
                    no_new_limit,
                    len(links),
                    scroll_state,
                )

                # IntersectionObserver ve lazy-load mekanizmasını tekrar
                # tetiklemek için kontrollü hareketler uygula.
                if stagnant % 4 == 0:
                    try:
                        await page.keyboard.press("End")
                        await page.mouse.wheel(0, 3500)
                    except Exception:
                        pass

                if stagnant % 8 == 0:
                    try:
                        await page.evaluate("""
() => {
    window.scrollBy(0, -900);
    window.scrollBy(0, 1800);
}
""")
                    except Exception:
                        pass

            if stagnant >= no_new_limit:
                log.info(
                    "[full-sync] no more accessible posts "
                    "after %d attempts discovered=%d",
                    stagnant,
                    len(links),
                )
                break
        if not links: raise ScraperError("NO_PUBLIC_POSTS", "Profil açık ancak erişilebilir gönderi bulunamadı", 404)
        selected = links[:max_posts] if max_posts else links
        for link, shortcode in selected:
            if shortcode in known:
                # Daha önce kaydedilmiş gönderiyi atla fakat taramayı
                # bitirme. Böylece mevcut 10 kayıttan sonra 11-100
                # arasındaki eski gönderiler de bulunabilir.
                continue
            cached = network_posts.get(shortcode)
            if cached and cached.get("caption") and cached.get("media_url") and cached.get("published_at"):
                yield cached
                continue
            detail = await context.new_page()
            try:
                detail.set_default_timeout(5000)
                try:
                    await detail.goto(link, wait_until="domcontentloaded", timeout=60_000)
                except PlaywrightTimeoutError:
                    if not await detail.locator("body").count():
                        log.warning("[instagram-browser] post timed out shortcode=%s", shortcode)
                        continue
                    log.info("[instagram-browser] post goto timed out but DOM is available shortcode=%s", shortcode)
                await detail.wait_for_timeout(750)
                caption = ""
                for selector in ('meta[property="og:description"]', "article", "main"):
                    try:
                        loc = detail.locator(selector).first
                        caption = (await loc.get_attribute("content") if selector.startswith("meta") else await loc.inner_text(timeout=3000)) or ""
                        if caption: break
                    except Exception: pass
                async def safe_attr(selector, name):
                    try:
                        locator = detail.locator(selector).first
                        return (await locator.get_attribute(name, timeout=2000) or "") if await locator.count() else ""
                    except Exception:
                        return ""
                image = await safe_attr('meta[property="og:image"]', "content")
                video = await safe_attr('meta[property="og:video"]', "content")
                published = await safe_attr("time", "datetime")
                yield {"external_id":shortcode,"shortcode":shortcode,"username":username,"caption":caption,
                       "published_at":published or datetime.now(timezone.utc).isoformat(),"permalink":link,
                       "media_type":"VIDEO" if video or "/reel/" in link else "IMAGE","thumbnail_url":image,"media_url":video or image,
                       "likes_count":0,"comments_count":0,"is_pinned":False}
            except Exception as exc:
                log.warning("[instagram-browser] post skipped shortcode=%s error=%s", shortcode, type(exc).__name__)
            finally:
                try: await detail.close()
                except Exception: pass


class InstagrapiPublicProvider:
    name = "instagrapi_public"
    async def scrape(self, username, known, max_posts):
        from instagrapi import Client
        client = Client(); info = await asyncio.to_thread(client.user_info_by_username_gql, username)
        medias = await asyncio.to_thread(client.user_medias_gql, info.pk, max_posts)
        for media in medias:
            if media.code in known: continue
            yield {"external_id":str(media.pk),"shortcode":media.code,"username":username,"caption":media.caption_text or "",
                   "published_at":media.taken_at.isoformat(),"permalink":f"https://www.instagram.com/p/{media.code}/","media_type":str(media.media_type),
                   "thumbnail_url":str(media.thumbnail_url or ""),"media_url":str(media.thumbnail_url or ""),"likes_count":media.like_count or 0,
                   "comments_count":media.comment_count or 0,"is_pinned":False}


class InstaloaderSessionProvider:
    name = "instaloader_session"
    async def scrape(self, username, known, max_posts):
        raise ScraperError("PROVIDER_UNAVAILABLE", "Instaloader session provider bu çalıştırmada etkin değil", 503)


_BROWSER_PROVIDER = None

def get_provider():
    global _BROWSER_PROVIDER

    name = os.getenv("INSTAGRAM_PROVIDER", "browser_public").lower()

    if name == "browser_public":
        if _BROWSER_PROVIDER is None:
            _BROWSER_PROVIDER = BrowserPublicProvider()
        return _BROWSER_PROVIDER

    providers = {
        "instagrapi_public": InstagrapiPublicProvider,
        "instaloader_session": InstaloaderSessionProvider,
    }

    if name not in providers:
        raise ScraperError(
            "PROVIDER_UNAVAILABLE",
            f"Bilinmeyen Instagram provider: {name}",
            503,
        )

    return providers[name]()
