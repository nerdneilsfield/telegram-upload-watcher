from __future__ import annotations

import argparse
import logging
import multiprocessing
import re
import shutil
import time
from io import BytesIO
from pathlib import Path

from PIL import Image
from rich.console import Console
from rich.table import Table
from tqdm import tqdm

_SIZE_RE = re.compile(r"^\s*(\d+)\s*([KMG]?B?)?\s*$", re.IGNORECASE)
_SIZE_UNITS = {
    "": 1,
    "B": 1,
    "K": 1024,
    "KB": 1024,
    "M": 1024 * 1024,
    "MB": 1024 * 1024,
    "G": 1024 * 1024 * 1024,
    "GB": 1024 * 1024 * 1024,
}


def _default_workers() -> int:
    try:
        return max(1, multiprocessing.cpu_count())
    except NotImplementedError:
        return 1


def _parse_size(value: str) -> int:
    match = _SIZE_RE.match(value)
    if not match:
        raise argparse.ArgumentTypeError(f"Invalid size format: {value!r}")
    number = int(match.group(1))
    unit = (match.group(2) or "").upper()
    if unit not in _SIZE_UNITS:
        raise argparse.ArgumentTypeError(f"Invalid size unit: {value!r}")
    return number * _SIZE_UNITS[unit]


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
    image: Image.Image, max_bytes: int, start_level: int
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


def _compress_png_to_limit(
    image: Image.Image, max_bytes: int, start_level: int
) -> tuple[bytes, int, bool]:
    resized = False
    png_bytes, level = _compress_png_greedy(image, max_bytes, start_level)
    if len(png_bytes) <= max_bytes:
        return png_bytes, level, resized

    while len(png_bytes) > max_bytes:
        current_max = max(image.size)
        if current_max <= 1:
            break
        scale = (max_bytes / len(png_bytes)) ** 0.5 * 0.95
        new_max = max(1, int(current_max * scale))
        if new_max >= current_max:
            new_max = current_max - 1
        if new_max < 1:
            break
        image.thumbnail((new_max, new_max), Image.Resampling.LANCZOS)
        resized = True
        png_bytes, level = _compress_png_greedy(image, max_bytes, start_level)
    return png_bytes, level, resized


def _should_copy(image_path: Path, max_dimension: int, max_bytes: int) -> bool:
    if image_path.stat().st_size > max_bytes:
        return False
    with Image.open(image_path) as image:
        return max(image.size) <= max_dimension


def _process_image(
    image_path: Path,
    dest_path: Path,
    *,
    max_dimension: int,
    max_bytes: int,
    png_start_level: int,
) -> tuple[str, float]:
    start = time.perf_counter()
    with Image.open(image_path) as image:
        original_format = image.format or "PNG"
        resized = _resize_if_needed(image, max_dimension)
        compressed = False
        output_name = image_path.name

        if resized:
            prepared = _prepare_for_format(image, original_format)
            encoded = _encode_image(prepared, original_format)
            if len(encoded) <= max_bytes:
                output_bytes = encoded
            else:
                compressed = True
                output_bytes, level, resized_more = _compress_png_to_limit(
                    image, max_bytes, png_start_level
                )
                resized = resized or resized_more
                output_name = f"{image_path.stem}.png"
                if len(output_bytes) > max_bytes:
                    logging.warning(
                        "PNG still over limit after resize: %s (%d > %d, level %d)",
                        image_path,
                        len(output_bytes),
                        max_bytes,
                        level,
                    )
        else:
            compressed = True
            output_bytes, level, resized_more = _compress_png_to_limit(
                image, max_bytes, png_start_level
            )
            resized = resized or resized_more
            output_name = f"{image_path.stem}.png"
            if len(output_bytes) > max_bytes:
                logging.warning(
                    "PNG still over limit after resize: %s (%d > %d, level %d)",
                    image_path,
                    len(output_bytes),
                    max_bytes,
                    level,
                )

    dest_path = dest_path.with_name(output_name)
    dest_path.parent.mkdir(parents=True, exist_ok=True)
    dest_path.write_bytes(output_bytes)

    elapsed = time.perf_counter() - start
    if resized and compressed:
        category = "scaled+compressed"
    elif resized:
        category = "scaled"
    elif compressed:
        category = "compressed"
    else:
        category = "copied"
    return category, elapsed


def _copy_image(image_path: Path, dest_path: Path) -> None:
    dest_path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(image_path, dest_path)


def _iter_image_files(root: Path) -> list[Path]:
    return [path for path in root.rglob("*") if path.is_file()]


def _handle_file(
    task: tuple[Path, Path, Path, int, int, int]
) -> tuple[str, float, Path, str | None]:
    path, in_dir, out_dir, max_dimension, max_bytes, png_start_level = task
    rel_path = path.relative_to(in_dir)
    dest_path = out_dir / rel_path
    try:
        start = time.perf_counter()
        if _should_copy(path, max_dimension, max_bytes):
            _copy_image(path, dest_path)
            elapsed = time.perf_counter() - start
            return "copied", elapsed, path, None
        category, elapsed = _process_image(
            path,
            dest_path,
            max_dimension=max_dimension,
            max_bytes=max_bytes,
            png_start_level=png_start_level,
        )
        return category, elapsed, path, None
    except OSError as exc:
        return "skipped", 0.0, path, str(exc)
    except Exception as exc:  # pragma: no cover - unexpected error path
        return "failed", 0.0, path, str(exc)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Compress images in a folder into an output folder."
    )
    parser.add_argument("in_dir", type=Path, help="Input directory path")
    parser.add_argument("out_dir", type=Path, help="Output directory path")
    parser.add_argument(
        "--max-dimension",
        type=int,
        default=2000,
        help="Maximum long-edge dimension before scaling",
    )
    parser.add_argument(
        "--max-size",
        type=_parse_size,
        default=_parse_size("1M"),
        help="Maximum image size (e.g. 1M, 700K)",
    )
    parser.add_argument(
        "--png-start-level",
        type=int,
        default=8,
        help="Initial PNG compress level for greedy search (0-9)",
    )
    parser.add_argument(
        "--workers",
        type=int,
        default=_default_workers(),
        help="Worker process count (>=1)",
    )
    parser.add_argument(
        "--log-level",
        default="INFO",
        help="Logging level (DEBUG, INFO, WARNING, ERROR)",
    )
    return parser


def run(args: argparse.Namespace) -> None:
    in_dir = args.in_dir
    out_dir = args.out_dir

    if not in_dir.exists():
        raise SystemExit(f"Input directory not found: {in_dir}")
    if not in_dir.is_dir():
        raise SystemExit(f"Input path is not a directory: {in_dir}")
    out_dir.mkdir(parents=True, exist_ok=True)

    paths = _iter_image_files(in_dir)
    tasks = [
        (path, in_dir, out_dir, args.max_dimension, args.max_size, args.png_start_level)
        for path in paths
    ]

    stats = {
        "copied": {"count": 0, "total": 0.0},
        "scaled": {"count": 0, "total": 0.0},
        "scaled+compressed": {"count": 0, "total": 0.0},
        "compressed": {"count": 0, "total": 0.0},
    }
    skipped = 0
    failed = 0

    workers = max(1, int(args.workers))
    if workers == 1:
        results = (
            _handle_file(task)
            for task in tqdm(tasks, desc="Compressing", unit="file")
        )
    else:
        with multiprocessing.Pool(processes=workers) as pool:
            results = tqdm(
                pool.imap_unordered(_handle_file, tasks),
                total=len(tasks),
                desc="Compressing",
                unit="file",
            )
            for result in results:
                category, elapsed, path, message = result
                if category in stats:
                    stats[category]["count"] += 1
                    stats[category]["total"] += elapsed
                    continue
                if category == "skipped":
                    skipped += 1
                    logging.warning("Skip non-image file %s: %s", path, message)
                elif category == "failed":
                    failed += 1
                    logging.error("Failed to process %s: %s", path, message)
            _print_summary(stats, skipped, failed)
            return

    for category, elapsed, path, message in results:
        if category in stats:
            stats[category]["count"] += 1
            stats[category]["total"] += elapsed
            continue
        if category == "skipped":
            skipped += 1
            logging.warning("Skip non-image file %s: %s", path, message)
        elif category == "failed":
            failed += 1
            logging.error("Failed to process %s: %s", path, message)
    _print_summary(stats, skipped, failed)


def _print_summary(stats: dict[str, dict[str, float]], skipped: int, failed: int) -> None:
    console = Console()
    table = Table(title="Compression Summary")
    table.add_column("Category")
    table.add_column("Count", justify="right")
    table.add_column("Avg time (ms)", justify="right")

    labels = {
        "copied": "Copied (unchanged)",
        "scaled": "Scaled only",
        "scaled+compressed": "Scaled + compressed",
        "compressed": "Compressed only",
    }

    for key in ("copied", "scaled", "scaled+compressed", "compressed"):
        count = int(stats[key]["count"])
        total = stats[key]["total"]
        avg_ms = (total / count * 1000.0) if count else 0.0
        table.add_row(labels[key], str(count), f"{avg_ms:.1f}")

    console.print(table)
    if skipped:
        console.print(f"Skipped non-image files: {skipped}")
    if failed:
        console.print(f"Failed files: {failed}")


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    logging.basicConfig(
        level=getattr(logging, args.log_level.upper(), logging.INFO),
        format="%(asctime)s - %(levelname)s - %(message)s",
    )
    run(args)


if __name__ == "__main__":
    main()
