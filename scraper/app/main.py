from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse

from .models import ScrapeRequest
from .scraper import normalize_profile, stream_profile

app = FastAPI(title="Instagram Instaloader Scraper")


@app.get("/health")
def health():
    return {"status": "ok", "provider": "instaloader"}


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
