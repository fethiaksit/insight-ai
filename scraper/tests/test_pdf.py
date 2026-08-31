import io

import pymupdf
from fastapi.testclient import TestClient

from app import pdf as pdf_module
from app.main import app


client = TestClient(app)


def make_pdf(text: str = "") -> bytes:
    document = pymupdf.open()
    page = document.new_page()
    if text:
        page.insert_text((72, 72), text)
    data = document.tobytes()
    document.close()
    return data


def test_extracts_text_per_page():
    payload = make_pdf("IZMIR Belediyesi normal metin belgesi ve arama icerigi")
    response = client.post("/v1/pdf/extract", files={"file": ("ornek.pdf", io.BytesIO(payload), "application/pdf")})

    assert response.status_code == 200
    assert response.json()["filename"] == "ornek.pdf"
    assert response.json()["page_count"] == 1
    assert "IZMIR Belediyesi" in response.json()["pages"][0]["text"]


def test_uses_ocr_only_when_text_is_not_meaningful(monkeypatch):
    calls = []

    def fake_ocr(page):
        calls.append(page.number)
        return "OCR ile çıkarılan yeterince uzun örnek sayfa metni"

    monkeypatch.setattr(pdf_module, "_extract_with_ocr", fake_ocr)
    response = client.post("/v1/pdf/extract", files={"file": ("tarama.pdf", io.BytesIO(make_pdf()), "application/pdf")})

    assert response.status_code == 200
    assert calls == [0]
    assert response.json()["pages"][0]["text"].startswith("OCR ile")


def test_rejects_non_pdf_content():
    response = client.post("/v1/pdf/extract", files={"file": ("sahte.pdf", io.BytesIO(b"not a pdf"), "application/pdf")})
    assert response.status_code == 400
