import sys
import instaloader

USERNAME = "omereski"
MAX_POSTS = 5

loader = instaloader.Instaloader(
    download_pictures=False,
    download_videos=False,
    download_video_thumbnails=False,
    save_metadata=False,
    compress_json=False,
    download_comments=False,
)

try:
    profile = instaloader.Profile.from_username(
        loader.context,
        USERNAME,
    )

    print(f"\nHesap: @{profile.username}")
    print(f"Profil adı: {profile.full_name}")
    print(f"Gizli hesap: {profile.is_private}")
    print(f"Toplam gönderi: {profile.mediacount}\n")

    if profile.is_private:
        print("HATA: Hesap gizli olduğu için gönderiler alınamaz.")
        sys.exit(1)

    count = 0

    for post in profile.get_posts():
        count += 1

        print("=" * 70)
        print(f"Gönderi: {count}")
        print(f"Shortcode: {post.shortcode}")
        print(f"Tarih: {post.date_utc}")
        print(f"Video: {post.is_video}")
        print(f"Link: https://www.instagram.com/p/{post.shortcode}/")
        print("\nAçıklama:")
        print(post.caption or "[Açıklama bulunmuyor]")
        print()

        if count >= MAX_POSTS:
            break

    if count == 0:
        print("Gönderi bulunamadı.")
        sys.exit(1)

    print(f"\nBaşarılı: {count} gönderi alındı.")

except instaloader.exceptions.ProfileNotExistsException:
    print(f"HATA: @{USERNAME} hesabı bulunamadı.")
    sys.exit(1)

except instaloader.exceptions.LoginRequiredException:
    print("HATA: Instagram anonim erişim için giriş istiyor.")
    sys.exit(1)

except instaloader.exceptions.ConnectionException as exc:
    print(f"HATA: Instagram bağlantı hatası: {exc}")
    sys.exit(1)

except Exception as exc:
    print(f"BEKLENMEYEN HATA: {type(exc).__name__}: {exc}")
    sys.exit(1)
