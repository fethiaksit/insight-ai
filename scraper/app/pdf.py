from pathlib import Path

import pymupdf
from fastapi import HTTPException, UploadFile


MAX_PDF_SIZE = 25 * 1024 * 1024


def extract_pdf(upload: UploadFile) -> dict:
    filename = Path(upload.filename or "document.pdf").name
    if Path(filename).suffix.lower() != ".pdf":
        raise HTTPException(status_code=400, detail="Yalnızca PDF dosyaları kabul edilir")

    data = upload.file.read(MAX_PDF_SIZE + 1)
    if len(data) > MAX_PDF_SIZE:
        raise HTTPException(status_code=413, detail="PDF dosyası 25 MB sınırını aşıyor")
    if not data or b"%PDF-" not in data[:1024]:
        raise HTTPException(status_code=400, detail="Dosya içeriği PDF değil")

    try:
        document = pymupdf.open(stream=data, filetype="pdf")
    except Exception as exc:
        raise HTTPException(status_code=400, detail="PDF dosyası açılamadı") from exc

    with document:
        if document.needs_pass:
            raise HTTPException(status_code=400, detail="Şifreli PDF dosyaları desteklenmiyor")
        if document.page_count < 1:
            raise HTTPException(status_code=400, detail="PDF en az bir sayfa içermeli")

        pages = []
        for page_number, page in enumerate(document, start=1):
            text = page.get_text("text", sort=True).strip()
            if not _has_meaningful_text(text):
                text = _extract_with_ocr(page).strip()
            pages.append({"page": page_number, "text": text})

    return {"filename": filename, "page_count": len(pages), "pages": pages}


def _has_meaningful_text(text: str) -> bool:
    return sum(character.isalnum() for character in text) >= 20


def _extract_with_ocr(page: pymupdf.Page) -> str:
    try:
        text_page = page.get_textpage_ocr(language="tur+eng", dpi=200, full=True)
        return page.get_text("text", textpage=text_page, sort=True)
    except Exception as exc:
        raise HTTPException(
            status_code=503,
            detail="OCR çalıştırılamadı; Tesseract ve Türkçe/İngilizce dil paketlerini kontrol edin",
        ) from exc
