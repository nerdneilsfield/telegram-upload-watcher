from __future__ import annotations

import logging
from io import BytesIO
from pathlib import Path

from PIL import Image


def _prepare_for_format(image: Image.Image, fmt: str) -> Image.Image:
    if fmt.upper() == "JPEG" and image.mode in {"RGBA", "LA", "P"}:
        return image.convert("RGB")
    return image


def _encode_image(image: Image.Image, fmt: str, **kwargs: int) -> bytes:
    buffer = BytesIO()
    image.save(buffer, format=fmt, **kwargs)
    return buffer.getvalue()


def _resize_if_needed(image: Image.Image, max_dimension: int) -> bool:
    if max(image.size) <= max_dimension:
        return False
    image.thumbnail((max_dimension, max_dimension), Image.Resampling.LANCZOS)
    return True


def _compress_png_greedy(
    image: Image.Image, max_bytes: int, start_level: int = 8
) -> tuple[bytes, int]:
    start_level = max(0, min(9, start_level))

    def encode(level: int) -> bytes:
        return _encode_image(image, "PNG", compress_level=level)

    data = encode(start_level)
    if len(data) > max_bytes:
        best = data
        best_level = start_level
        for level in range(start_level + 1, 10):
            data = encode(level)
            best = data
            best_level = level
            if len(data) <= max_bytes:
                return data, level
        return best, best_level

    best = data
    best_level = start_level
    for level in range(start_level - 1, -1, -1):
        data = encode(level)
        if len(data) <= max_bytes:
            best = data
            best_level = level
            continue
        break
    return best, best_level


def prepare_image_bytes(
    data: bytes,
    filename: str,
    *,
    max_dimension: int,
    max_bytes: int,
    png_start_level: int = 8,
) -> tuple[bytes, str]:
    with Image.open(BytesIO(data)) as image:
        original_format = image.format or "PNG"
        _resize_if_needed(image, max_dimension)

        working = _prepare_for_format(image, original_format)
        encoded = _encode_image(working, original_format)
        if len(encoded) <= max_bytes:
            return encoded, filename

        png_bytes, level = _compress_png_greedy(image, max_bytes, png_start_level)
        logging.info("PNG compressed using level %d for %s", level, filename)
        new_name = f"{Path(filename).stem}.png"
        return png_bytes, new_name
