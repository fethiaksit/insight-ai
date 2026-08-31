from pydantic import BaseModel, Field


class ScrapeRequest(BaseModel):
    profile: str
    known_shortcodes: list[str] = Field(default_factory=list)
    full_history: bool = True
    max_posts: int | None = Field(default=2500, ge=0, le=2500)
    batch_size: int = Field(default=50, ge=1, le=500)
