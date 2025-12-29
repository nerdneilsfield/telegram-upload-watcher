from __future__ import annotations

import argparse
import logging
import multiprocessing
import re
import shutil
from pathlib import Path

from PIL import Image
from tqdm import tqdm

from telegram_upload_watcher.image_processing import prepare_image_bytes

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
) -> Path:
    data = image_path.read_bytes()
    out_bytes, out_name = prepare_image_bytes(
        data,
        image_path.name,
        max_dimension=max_dimension,
        max_bytes=max_bytes,
        png_start_level=png_start_level,
    )
    dest_path = dest_path.with_name(out_name)
    dest_path.parent.mkdir(parents=True, exist_ok=True)
    dest_path.write_bytes(out_bytes)
    return dest_path


def _copy_image(image_path: Path, dest_path: Path) -> None:
    dest_path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(image_path, dest_path)


def _iter_image_files(root: Path) -> list[Path]:
    return [path for path in root.rglob("*") if path.is_file()]


def _handle_file(task: tuple[Path, Path, Path, int, int, int]) -> tuple[str, Path, str | None]:
    path, in_dir, out_dir, max_dimension, max_bytes, png_start_level = task
    rel_path = path.relative_to(in_dir)
    dest_path = out_dir / rel_path
    try:
        if _should_copy(path, max_dimension, max_bytes):
            _copy_image(path, dest_path)
            return "copied", path, None
        final_path = _process_image(
            path,
            dest_path,
            max_dimension=max_dimension,
            max_bytes=max_bytes,
            png_start_level=png_start_level,
        )
        return "processed", path, str(final_path)
    except OSError as exc:
        return "skipped", path, str(exc)
    except Exception as exc:  # pragma: no cover - unexpected error path
        return "error", path, str(exc)


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
                if result[0] == "skipped":
                    logging.warning("Skip non-image file %s: %s", result[1], result[2])
                elif result[0] == "error":
                    logging.error("Failed to process %s: %s", result[1], result[2])
            return

    for result in results:
        if result[0] == "skipped":
            logging.warning("Skip non-image file %s: %s", result[1], result[2])
        elif result[0] == "error":
            logging.error("Failed to process %s: %s", result[1], result[2])


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
