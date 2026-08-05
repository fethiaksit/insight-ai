import json
import asyncio

from app import scraper


class FakeProvider:
    name = "browser_public"
    async def scrape(self, username, known, max_posts):
        yield {"external_id":"1", "shortcode":"abc", "username":username, "caption":"", "published_at":"2026-01-01T00:00:00Z", "permalink":"https://www.instagram.com/p/abc/", "media_type":"IMAGE", "thumbnail_url":"", "media_url":"", "likes_count":0, "comments_count":0, "is_pinned":False}


def test_ndjson_stream_keeps_blank_caption(monkeypatch):
    monkeypatch.setattr(scraper, "get_provider", lambda: FakeProvider())
    async def collect(): return [json.loads(line) async for line in scraper.stream_profile("omereski", [], 20)]
    rows = asyncio.run(collect())
    assert [row["type"] for row in rows] == ["profile", "post", "complete"]
    assert rows[1]["post"]["caption"] == ""
    assert rows[-1]["provider"] == "browser_public"
