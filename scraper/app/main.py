from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse

from .models import ScrapeRequest
from .scraper import normalize_profile, stream_profile
from .providers import BrowserPublicProvider, get_provider

app = FastAPI(title="Instagram Instaloader Scraper")


@app.get("/health")
def health():
    provider = get_provider()
    state = provider.status() if isinstance(provider, BrowserPublicProvider) else {"browser_profile_ready": False, "instagram_authenticated": False}
    return {"status": "ok", "provider": provider.name, **state}

@app.post("/browser/open")
def browser_open():
    provider = BrowserPublicProvider(); provider.open()
    return {"status":"opened", **provider.status()}

@app.get("/browser/status")
def browser_status():
    return BrowserPublicProvider().status()


@app.post("/v1/profiles/scrape")
def scrape(request: ScrapeRequest):
    try:
        username = normalize_profile(request.profile)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail={"code": "INVALID_PROFILE", "message": str(exc)}) from exc
    return StreamingResponse(
        stream_profile(username, request.known_shortcodes, request.max_posts),
        media_type="application/x-ndjson",
    )
