import asyncio

from app.providers import wait_for_instagram_page_state


class Locator:
    def __init__(self, count=0, text=""): self._count, self._text = count, text
    async def count(self): return self._count
    async def inner_text(self, timeout=0): return self._text


class Page:
    def __init__(self, url, posts=0, shell=0, login=0, body=""):
        self.url, self.posts, self.shell, self.login, self.body = url, posts, shell, login, body
    def locator(self, selector):
        if 'a[href*=' in selector: return Locator(self.posts)
        if 'input[name=' in selector: return Locator(self.login)
        if selector == "body": return Locator(1, self.body)
        return Locator(self.shell)


def state(page): return asyncio.run(wait_for_instagram_page_state(page, 20))


def test_post_link_wins_even_after_navigation_timeout(): assert state(Page("https://instagram.com/u/", posts=1)) == "profile_ready"
def test_login_page(): assert state(Page("https://instagram.com/accounts/login/", login=1)) == "login_required"
def test_challenge_page(): assert state(Page("https://instagram.com/challenge/")) == "challenge_required"
def test_empty_loaded_profile(): assert state(Page("https://instagram.com/u/", shell=1)) == "page_loaded_without_posts"
