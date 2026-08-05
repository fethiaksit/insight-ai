import json
import re
from datetime import timezone
from urllib.parse import unquote, urlparse

import instaloader

from .errors import ScraperError

USERNAME = re.compile(r"^[A-Za-z0-9._]{1,30}$")
RESERVED = {"p", "reel", "reels", "stories", "explore", "accounts", "direct"}


def normalize_profile(value: str) -> str:
    value = (value or "").strip()
    if not value:
        raise ValueError("Instagram kullanıcı adı veya profil URL'si gerekli")
    if "://" in value:
        parsed = urlparse(value)
        if (parsed.hostname or "").lower().removeprefix("www.") != "instagram.com":
            raise ValueError("Yalnızca instagram.com profil URL'leri kabul edilir")
        parts = [unquote(x) for x in parsed.path.strip("/").split("/") if x]
        if len(parts) != 1 or parts[0].lower() in RESERVED:
            raise ValueError("URL bir Instagram profilini göstermiyor")
        value = parts[0]
    elif any(x in value for x in "/?#"):
        raise ValueError("Instagram kullanıcı adı geçersiz")
    value = value.removeprefix("@").strip().lower()
    if not USERNAME.fullmatch(value) or value in RESERVED:
        raise ValueError("Instagram kullanıcı adı geçersiz")
    return value


def _line(payload: dict) -> bytes:
    return (json.dumps(payload, ensure_ascii=False) + "\n").encode()


def _error(exc: Exception) -> ScraperError:
    if isinstance(exc, instaloader.exceptions.ProfileNotExistsException):
        return ScraperError("PROFILE_NOT_FOUND", "Instagram profili bulunamadı", 404)
    if isinstance(exc, instaloader.exceptions.LoginRequiredException):
        return ScraperError("INSTAGRAM_LOGIN_REQUIRED", "Instagram bu profil için giriş yapılmasını istiyor", 403)
    if isinstance(exc, instaloader.exceptions.TooManyRequestsException):
        return ScraperError("INSTAGRAM_RATE_LIMITED", "Instagram istek sınırına ulaşıldı; daha sonra tekrar deneyin", 429)
    if isinstance(exc, instaloader.exceptions.ConnectionException):
        message = str(exc).lower()
        if "please wait" in message or "429" in message or "too many" in message:
            return ScraperError("INSTAGRAM_RATE_LIMITED", "Instagram istek sınırına ulaşıldı; daha sonra tekrar deneyin", 429)
        if "login" in message or "checkpoint" in message:
            return ScraperError("INSTAGRAM_LOGIN_REQUIRED", "Instagram erişimi engelledi veya giriş istiyor", 403)
        if "timed out" in message or "timeout" in message:
            return ScraperError("INSTAGRAM_TIMEOUT", "Instagram isteği zaman aşımına uğradı", 504)
        return ScraperError("INSTAGRAM_CONNECTION_ERROR", f"Instagram bağlantısı kurulamadı: {exc}", 502)
    return ScraperError("SCRAPER_INTERNAL_ERROR", "Instagram verileri işlenirken hata oluştu", 500)


def stream_profile(username: str, known_shortcodes: list[str], max_posts: int):
    loader = instaloader.Instaloader(
        download_pictures=False, download_videos=False,
        download_video_thumbnails=False, save_metadata=False,
        compress_json=False, download_comments=False,
    )
    try:
        profile = instaloader.Profile.from_username(loader.context, username)
        if profile.is_private:
            raise ScraperError("PRIVATE_PROFILE", "Gizli Instagram profilleri taranamaz", 403)
        yield _line({"type": "profile", "profile": {
            "username": profile.username.lower(), "full_name": profile.full_name or "",
            "profile_pic_url": profile.profile_pic_url or "", "is_private": profile.is_private,
            "posts_count": profile.mediacount,
        }})
        known, consecutive, fetched, stopped = set(known_shortcodes), 0, 0, False
        for post in profile.get_posts():
            if post.shortcode in known:
                consecutive += 1
                if consecutive >= 3:
                    stopped = True
                    break
                continue
            consecutive = 0
            if max_posts and fetched >= max_posts:
                break
            published = post.date_utc.replace(tzinfo=timezone.utc).isoformat().replace("+00:00", "Z")
            media_type = "VIDEO" if post.is_video else ("CAROUSEL_ALBUM" if post.typename == "GraphSidecar" else "IMAGE")
            media_url = post.video_url if post.is_video else post.url
            yield _line({"type": "post", "post": {
                "external_id": str(post.mediaid), "shortcode": post.shortcode,
                "username": username, "caption": post.caption or "", "published_at": published,
                "permalink": f"https://www.instagram.com/p/{post.shortcode}/",
                "media_type": media_type, "media_url": media_url or "",
                "thumbnail_url": post.url or "", "is_video": post.is_video,
                "likes_count": post.likes, "comments_count": post.comments,
            }})
            fetched += 1
        yield _line({"type": "complete", "fetched": fetched, "stopped_on_known_post": stopped})
    except ScraperError as exc:
        yield _line({"type": "error", "error": {"code": exc.code, "message": exc.message}})
    except Exception as exc:
        error = _error(exc)
        yield _line({"type": "error", "error": {"code": error.code, "message": error.message}})
