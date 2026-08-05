from pydantic import BaseModel, Field


class ScrapeRequest(BaseModel):
    profile: str
    known_shortcodes: list[str] = Field(default_factory=list)
    max_posts: int = Field(default=0, ge=0)
