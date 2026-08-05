import pytest

from app.scraper import normalize_profile


@pytest.mark.parametrize("value", ["omereski", "@omereski", " https://www.instagram.com/omereski/?hl=tr "])
def test_normalizes_supported_inputs(value):
    assert normalize_profile(value) == "omereski"


@pytest.mark.parametrize("value", ["", "https://example.com/a", "https://instagram.com/p/abc", "https://instagram.com/reel/abc", "bad/name"])
def test_rejects_non_profiles(value):
    with pytest.raises(ValueError):
        normalize_profile(value)
