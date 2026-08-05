# AI Destekli Sosyal Medya Analiz Platformu

Bu sürüm yalnızca **Redis** kullanır. Hesaplar, gönderiler, AI analizleri, konular, anahtar kelimeler ve ayarlar Redis'te JSON belgeleri olarak saklanır.

## Docker

```bash
docker compose up -d --build
curl http://localhost:8080/health
```

Compose yalnızca iki servis başlatır: `backend` ve `redis`. Redis kalıcı verisi `redis_data` volume'ünde tutulur. Backend Redis healthcheck tamamlanana kadar başlamaz ve ayrıca Redis bağlantısını yeniden dener.

## Yerel geliştirme

Önce Redis'i açın:

```bash
docker compose up -d redis
```

Backend root dizinden çalışır:

```bash
go run ./backend/cmd/api
```

Yerel backend varsayılan olarak `redis://localhost:6379/0` kullanır. Frontend bağımsız olarak geliştirme modunda çalışır:

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

## Instagram provider

Instagram Login zorunlu değildir. Uygulama, yalnızca seçtiğiniz herkese açık hesapların sağlayıcı tarafından erişilebilen gönderilerini toplar. Kullanıcı adı/şifre, gizli cookie ve tarayıcı otomasyonu kullanılmaz. Sağlayıcı yoksa sahte veri üretilmez.

```bash
INSTAGRAM_PROVIDER=generic-http
INSTAGRAM_PROVIDER_API_KEY=...
INSTAGRAM_PROVIDER_BASE_URL=https://provider.example/v1
INSTAGRAM_SYNC_CRON="*/30 * * * *"
```

`external` adapter sözleşmesi şöyledir:

- `GET /profiles/{username}` → `username`, `name`, `profile_picture_url`
- `GET /profiles/{username}/posts?cursor=...` → `posts` ve `next_cursor`
- `GET /posts/{shortcode}` → tek gönderi

Gönderi alanları `external_id`, `caption`, `permalink`, `media_type`, `media_url`, `thumbnail_url` ve RFC3339 `published_at` değeridir. API anahtarı hem `Authorization: Bearer` hem de `X-API-Key` başlığıyla gönderilir; farklı bir vendor sözleşmesi için yalnızca yeni bir `InstagramProvider` adapter'ı gerekir.

Yeni hesap eklenince tüm `next_cursor` sayfaları tüketilir. Sonraki manuel ve zamanlanmış senkronizasyonlar aynı `external_id` veya `permalink` değerini yeniden kaydetmez. Hesaplar ve gönderiler Redis'te süresiz tutulur; Docker Redis servisi AOF `everysec` ile çalışır. OpenAI anahtarı Instagram anahtar kelime araması için gerekli değildir.

Arayüz geliştirmesi için `APP_ENV=development` ve `INSTAGRAM_PROVIDER=mock` açıkça seçilebilir. Bu sağlayıcı demo içerik üretir ve arayüzde “Demo veri” etiketi gösterilir. Production ortamında mock provider etkinleştirilmez ve hiçbir zaman varsayılan değildir.
