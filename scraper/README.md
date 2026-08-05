# Instaloader scraper

Kimlik bilgisi, cookie, proxy veya CAPTCHA atlatma kullanmadan herkese açık Instagram profillerini NDJSON olarak aktarır.

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --host 127.0.0.1 --port 8091
```

Test: `pytest`. Canlı deneme:

```bash
curl -N -X POST http://127.0.0.1:8091/v1/profiles/scrape -H 'Content-Type: application/json' -d '{"profile":"omereski","known_shortcodes":[],"max_posts":5}'
```
