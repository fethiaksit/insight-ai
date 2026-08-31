import json
import re
import asyncio
from urllib.parse import unquote, urlparse

from .errors import ScraperError
from .providers import get_provider

USERNAME = re.compile(r"^[A-Za-z0-9._]{1,30}$")
RESERVED = {"p", "reel", "reels", "stories", "explore", "accounts", "direct", "tv"}
SCRAPE_LOCK = asyncio.Lock()


def normalize_profile(value: str) -> str:
    value = (value or "").strip()
    if not value: raise ValueError("Instagram kullanıcı adı veya profil URL'si gerekli")
    if value.lower().startswith(("instagram.com/", "www.instagram.com/")): value = "https://" + value
    if "://" in value:
        parsed = urlparse(value)
        if (parsed.hostname or "").lower().removeprefix("www.") != "instagram.com": raise ValueError("Yalnızca instagram.com profil URL'leri kabul edilir")
        parts = [unquote(x) for x in parsed.path.strip("/").split("/") if x]
        if len(parts) != 1 or parts[0].lower() in RESERVED: raise ValueError("URL bir Instagram profilini göstermiyor")
        value = parts[0]
    elif any(x in value for x in "/?#"): raise ValueError("Instagram kullanıcı adı geçersiz")
    value = value.removeprefix("@").strip().lower()
    if not USERNAME.fullmatch(value) or value in RESERVED: raise ValueError("Instagram kullanıcı adı geçersiz")
    return value


def _line(payload): return (json.dumps(payload, ensure_ascii=False) + "\n").encode()


async def stream_profile(username, known_shortcodes, max_posts=0, full_history=True, batch_size=50):
    if SCRAPE_LOCK.locked():
        yield _line({"type":"error","error":{"code":"PROVIDER_UNAVAILABLE","message":"Başka bir Instagram senkronizasyonu çalışıyor","retry_after_seconds":60}}); return
    async with SCRAPE_LOCK:
      try:
        provider = get_provider()
        yield _line({"type":"profile","profile":{"username":username,"profile_url":f"https://www.instagram.com/{username}/"}})
        fetched = 0
        limit = min(int(max_posts or 2500), 2500)
        async for post in provider.scrape(username, set(known_shortcodes), limit):
            yield _line({"type":"post","post":post}); fetched += 1
            if fetched % 25 == 0:
                yield _line({"type":"progress","discovered":getattr(provider,"discovered_count",fetched),"processed":fetched,"saved":fetched,"updated":0,"skipped":0,"failed":0,"scroll_round":getattr(provider,"scroll_round",0),"status":"running"})
        # Bilinen shortcode verildiyse sıfır sonuç, profil boş demek değil;
        # yalnızca yeni gönderi olmadığı anlamına gelir ve başarılı tamamlanır.
        if fetched == 0 and not known_shortcodes: raise ScraperError("NO_PUBLIC_POSTS", "Erişilebilir herkese açık gönderi bulunamadı", 404)
        yield _line({"type":"complete","provider":provider.name,"discovered":getattr(provider,"discovered_count",fetched),"processed":fetched,"saved":fetched,"updated":0,"skipped":0,"failed":0,"fetched":fetched,"stop_reason":"profile_end_reached"})
      except ScraperError as exc:
        retry = 1800 if exc.code == "INSTAGRAM_RATE_LIMITED" else 0
        yield _line({"type":"error","error":{"code":exc.code,"message":exc.message,"retry_after_seconds":retry}})
      except Exception as exc:
        msg = str(exc)
        code = "INSTAGRAM_RATE_LIMITED" if "please wait" in msg.lower() or "429" in msg else "SCRAPE_FAILED"
        yield _line({"type":"error","error":{"code":code,"message":"Instagram geçici olarak çok fazla istek algıladı. 30 dakika sonra tekrar deneyin." if code == "INSTAGRAM_RATE_LIMITED" else f"Instagram gönderileri alınamadı: {msg}","retry_after_seconds":1800 if code == "INSTAGRAM_RATE_LIMITED" else 0}})
