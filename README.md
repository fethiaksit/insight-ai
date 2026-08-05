# AI Destekli Sosyal Medya Analiz Platformu

Bu sürüm yalnızca **Redis** kullanır. Hesaplar, gönderiler, AI analizleri, konular, anahtar kelimeler ve ayarlar Redis'te JSON belgeleri olarak saklanır.

## Docker

```bash
docker compose up -d --build
curl http://localhost:8080/health
```

Compose `scraper`, `backend` ve `redis` servislerini başlatır. Redis kalıcı verisi `redis_data` volume'ünde tutulur.

## Yerel geliştirme

Dört terminalde yerel geliştirme:

```bash
redis-server
```

```bash
cd scraper
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --host 127.0.0.1 --port 8091
```

```bash
cd backend
go run ./cmd/api
```

Yerel backend varsayılan olarak `redis://localhost:6379/0` ve scraper için `http://127.0.0.1:8091` kullanır.

```bash
cd frontend
npm install
npm run dev
```

## Uçlar

- `GET http://localhost:8080/health` — Redis bağlantı kontrolü
- `GET http://localhost:8080/api/v1/dashboard`
- `GET http://localhost:8080/swagger/openapi.yaml`

Tüm yapılandırma değerleri [`.env.example`](.env.example) içinde yer alır. `REDIS_URL` tanımlanırsa `REDIS_HOST` ve `REDIS_PORT` yerine önceliklidir.

## Instagram scraper

Instagram Login zorunlu değildir. Uygulama, yalnızca seçtiğiniz herkese açık hesapların sağlayıcı tarafından erişilebilen gönderilerini toplar. Kullanıcı adı/şifre, gizli cookie ve tarayıcı otomasyonu kullanılmaz. Sağlayıcı yoksa sahte veri üretilmez.

`INSTAGRAM_SCRAPER_URL` varsayılan olarak `http://127.0.0.1:8091`, zaman aşımı `INSTAGRAM_SCRAPER_TIMEOUT=30m` değeridir. Meta access token veya business account kimliği gerekmez.

Gönderi alanları `external_id`, `shortcode`, `caption`, `permalink`, `media_type`, `media_url`, `thumbnail_url` ve RFC3339 `published_at` değeridir. Scraper sonucu `application/x-ndjson` olarak aktarılır ve Go backend her gönderiyi akıştan geldiği anda Redis'e yazar.

Yeni hesap eklenince erişilebilen geçmiş gönderiler alınır. Sonraki manuel ve zamanlanmış senkronizasyonlarda bilinen shortcode'lar scraper'a iletilir; art arda üç bilinen gönderide akış durur. Aynı `external_id` veya permalink yeniden kaydedilmez. Hesaplar ve gönderiler Redis'te süresiz tutulur; Docker Redis servisi AOF `everysec` ile çalışır. OpenAI anahtarı Instagram anahtar kelime araması için gerekli değildir.

Canlı kontrol için scraper çalışırken `curl -N -X POST http://127.0.0.1:8091/v1/profiles/scrape -H 'Content-Type: application/json' -d '{"profile":"omereski","known_shortcodes":[],"max_posts":5}'` kullanılabilir. Instagram erişimi engellerse servis boş başarı yerine hata kodu içeren bir NDJSON satırı döndürür.
