from fastapi import FastAPI, File, HTTPException, UploadFile
from fastapi.responses import StreamingResponse

from .models import ScrapeRequest
from .pdf import extract_pdf
from .scraper import normalize_profile, stream_profile
from .providers import BrowserPublicProvider, get_provider

app = FastAPI(title="Instagram Instaloader Scraper")


@app.get("/health")
async def health():
    provider = get_provider()
    state = await provider.status() if isinstance(provider, BrowserPublicProvider) else {"browser_running": False, "instagram_authenticated": False}
    return {"status": "ok", "provider": provider.name, "browser_profile_ready": provider.profile_dir.exists() if isinstance(provider, BrowserPublicProvider) else False, **state}

@app.post("/browser/open")
async def browser_open():
    provider = get_provider(); await provider.open(navigate_home=True)
    return {"status":"opened", **(await provider.status())}

@app.get("/browser/status")
async def browser_status():
    provider = get_provider()
    return await provider.status()


@app.post("/v1/profiles/scrape")
def scrape(request: ScrapeRequest):
    try:
        username = normalize_profile(request.profile)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail={"code": "INVALID_PROFILE", "message": str(exc)}) from exc
    return StreamingResponse(
        stream_profile(username, request.known_shortcodes, request.max_posts, request.full_history, request.batch_size),
        media_type="application/x-ndjson",
    )


@app.post("/v1/pdf/extract")
def pdf_extract(file: UploadFile = File(...)):
    return extract_pdf(file)
