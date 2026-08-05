import os
import re
import threading
from datetime import datetime, timezone
from pathlib import Path

from .errors import ScraperError


class BrowserPublicProvider:
    name = "browser_public"
    _lock = threading.Lock()
    _playwright = None
    _context = None

    def __init__(self):
        self.profile_dir = Path(os.path.expandvars(os.path.expanduser(os.getenv(
            "INSTAGRAM_BROWSER_PROFILE_DIR", "~/.config/sosyalmedyatakip/chromium-profile"
        ))))
        self.headless = os.getenv("INSTAGRAM_BROWSER_HEADLESS", "false").lower() == "true"

    def open(self):
        with self._lock:
            cls = type(self)
            if cls._context:
                return cls._context
            try:
                from playwright.sync_api import sync_playwright
                self.profile_dir.mkdir(parents=True, exist_ok=True)
                cls._playwright = sync_playwright().start()
                cls._context = cls._playwright.chromium.launch_persistent_context(
                    str(self.profile_dir), headless=self.headless, viewport={"width": 1280, "height": 900}
                )
                return cls._context
            except Exception as exc:
                raise ScraperError("PROVIDER_UNAVAILABLE", f"Chromium başlatılamadı: {exc}", 503) from exc

    def status(self):
        ready = self.profile_dir.exists()
        authenticated = False
        if type(self)._context:
            authenticated = any(c.get("name") == "sessionid" for c in type(self)._context.cookies("https://www.instagram.com"))
        return {"browser_profile_ready": ready, "instagram_authenticated": authenticated}

    def scrape(self, username, known, max_posts):
        context = self.open()
        page = context.pages[0] if context.pages else context.new_page()
        page.set_default_timeout(10_000)
        url = f"https://www.instagram.com/{username}/"
        try:
            page.goto(url, wait_until="domcontentloaded", timeout=45_000)
            page.wait_for_timeout(3000)
        except Exception as exc:
            raise ScraperError("SCRAPE_TIMEOUT", "Instagram profil sayfası zaman aşımına uğradı", 504) from exc
        body = page.locator("body").inner_text().lower()
        if "sorry, this page isn't available" in body or "bu sayfaya ulaşılamıyor" in body:
            raise ScraperError("PROFILE_NOT_FOUND", "Instagram profili bulunamadı", 404)
        if "log in" in body and not self.status()["instagram_authenticated"]:
            raise ScraperError("BROWSER_LOGIN_REQUIRED", "Instagram oturumu gerekiyor. Açılan tarayıcıdan hesabınıza giriş yapın", 401)
        links = page.locator('a[href*="/p/"],a[href*="/reel/"],a[href*="/tv/"]').evaluate_all("els => [...new Set(els.map(e => e.href))]")
        yielded = 0
        for link in links:
            match = re.search(r"/(?:p|reel|tv)/([^/?#]+)", link)
            if not match: continue
            shortcode = match.group(1)
            if shortcode in known: break
            if yielded >= max_posts: break
            detail = context.new_page()
            try:
                detail.goto(link, wait_until="domcontentloaded", timeout=60_000)
                detail.wait_for_timeout(1200)
                caption = detail.locator('meta[property="og:description"]').get_attribute("content") or ""
                image = detail.locator('meta[property="og:image"]').get_attribute("content") or ""
                video = detail.locator('meta[property="og:video"]').get_attribute("content") or ""
                published = detail.locator("time").first.get_attribute("datetime") if detail.locator("time").count() else ""
                yield {"external_id": shortcode, "shortcode": shortcode, "username": username,
                       "caption": caption, "published_at": published or datetime.now(timezone.utc).isoformat(),
                       "permalink": link.split("?")[0], "media_type": "VIDEO" if video or "/reel/" in link else "IMAGE",
                       "thumbnail_url": image, "media_url": video or image, "likes_count": 0,
                       "comments_count": 0, "is_pinned": False}
                yielded += 1
            finally:
                detail.close()


class InstagrapiPublicProvider:
    name = "instagrapi_public"
    def scrape(self, username, known, max_posts):
        try:
            from instagrapi import Client
            client = Client()
            info = client.user_info_by_username_gql(username)
            for media in client.user_medias_gql(info.pk, amount=max_posts):
                code = media.code
                if code in known: break
                yield {"external_id": str(media.pk), "shortcode": code, "username": username,
                       "caption": media.caption_text or "", "published_at": media.taken_at.isoformat(),
                       "permalink": f"https://www.instagram.com/p/{code}/", "media_type": str(media.media_type),
                       "thumbnail_url": str(media.thumbnail_url or ""), "media_url": str(media.thumbnail_url or ""),
                       "likes_count": media.like_count or 0, "comments_count": media.comment_count or 0, "is_pinned": False}
        except Exception as exc:
            raise ScraperError("SCRAPE_FAILED", f"Instagram public web isteği başarısız: {exc}", 502) from exc


class InstaloaderSessionProvider:
    name = "instaloader_session"
    def scrape(self, username, known, max_posts):
        import instaloader
        session_file, session_user = os.getenv("INSTAGRAM_SESSION_FILE"), os.getenv("INSTAGRAM_SESSION_USERNAME")
        if not session_file or not session_user or not Path(session_file).is_file():
            raise ScraperError("PROVIDER_UNAVAILABLE", "Doğrulanmış Instaloader session dosyası bulunamadı", 503)
        loader = instaloader.Instaloader(download_pictures=False, download_videos=False, download_video_thumbnails=False, save_metadata=False, compress_json=False, download_comments=False)
        loader.load_session_from_file(session_user, session_file)
        if loader.test_login() != session_user:
            raise ScraperError("INSTAGRAM_LOGIN_REQUIRED", "Instaloader oturumu doğrulanamadı", 401)
        profile = instaloader.Profile.from_username(loader.context, username)
        if profile.is_private: raise ScraperError("PROFILE_PRIVATE", "Bu profil gizli olduğu için gönderiler alınamıyor", 403)
        for index, post in enumerate(profile.get_posts()):
            if post.shortcode in known: break
            if index >= max_posts: break
            yield {"external_id": str(post.mediaid), "shortcode": post.shortcode, "username": username,
                   "caption": post.caption or "", "published_at": post.date_utc.replace(tzinfo=timezone.utc).isoformat(),
                   "permalink": f"https://www.instagram.com/p/{post.shortcode}/", "media_type": "VIDEO" if post.is_video else "IMAGE",
                   "thumbnail_url": post.url or "", "media_url": post.video_url if post.is_video else post.url,
                   "likes_count": post.likes, "comments_count": post.comments, "is_pinned": False}


def get_provider():
    name = os.getenv("INSTAGRAM_PROVIDER", "browser_public").lower()
    providers = {"browser_public": BrowserPublicProvider, "instagrapi_public": InstagrapiPublicProvider, "instaloader_session": InstaloaderSessionProvider}
    if name not in providers: raise ScraperError("PROVIDER_UNAVAILABLE", f"Bilinmeyen Instagram provider: {name}", 503)
    return providers[name]()
