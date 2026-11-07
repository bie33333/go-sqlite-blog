#!/usr/bin/env python3

import argparse
import json
import re
import shutil
import sqlite3
import sys
import tempfile
from datetime import datetime, timezone
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlsplit


class ImageReferences(HTMLParser):
    def __init__(self):
        super().__init__()
        self.urls = []

    def handle_starttag(self, tag, attrs):
        if tag.lower() == "img":
            self.urls.extend(value for key, value in attrs if key.lower() == "src" and value)


def referenced_uploads(content):
    parser = ImageReferences()
    parser.feed(content)
    urls = parser.urls
    urls.extend(
        match.group(1).strip("<>")
        for match in re.finditer(r"!\[[^\]]*\]\(\s*(<?/uploads/[^)\s]+>?)", content)
    )

    filenames = set()
    for url in urls:
        path = unquote(urlsplit(url).path)
        if not path.startswith("/uploads/"):
            continue
        filename = path.removeprefix("/uploads/")
        if not filename or Path(filename).name != filename or filename in {".", ".."}:
            raise ValueError(f"unsafe upload reference: {url}")
        filenames.add(filename)
    return sorted(filenames)


def main():
    parser = argparse.ArgumentParser(description="Export published blog articles and referenced uploads.")
    parser.add_argument("--database", type=Path, default=Path("/data/blog.db"))
    parser.add_argument("--uploads", type=Path, default=Path("/uploads"))
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    if args.output.exists():
        parser.error(f"output directory already exists: {args.output}")

    with tempfile.TemporaryDirectory() as temp_dir:
        snapshot_path = Path(temp_dir) / "snapshot.db"
        source_uri = f"file:{args.database}?mode=ro"
        with sqlite3.connect(source_uri, uri=True) as source:
            with sqlite3.connect(snapshot_path) as snapshot:
                source.backup(snapshot)

        with sqlite3.connect(snapshot_path) as snapshot:
            rows = snapshot.execute(
                """
                SELECT id, title, content, created_at
                FROM articles
                WHERE approved = 1
                ORDER BY created_at ASC, id ASC
                """
            ).fetchall()

    if not rows:
        raise ValueError("no published articles found; refusing to create an empty backup")

    image_names = sorted(
        {filename for _, _, content, _ in rows for filename in referenced_uploads(content)}
    )
    args.output.mkdir(parents=True)
    try:
        uploads_output = args.output / "uploads"
        if image_names:
            uploads_output.mkdir()
            for filename in image_names:
                source_path = args.uploads / filename
                if source_path.is_symlink() or not source_path.is_file():
                    raise FileNotFoundError(f"referenced upload is missing or unsafe: {filename}")
                shutil.copyfile(source_path, uploads_output / filename)

        backup = {
            "format_version": 1,
            "exported_at_utc": datetime.now(timezone.utc).isoformat(timespec="seconds"),
            "articles": [
                {
                    "id": article_id,
                    "title": title,
                    "content": content,
                    "created_at": created_at,
                }
                for article_id, title, content, created_at in rows
            ],
            "images": image_names,
        }
        with (args.output / "articles.json").open("w", encoding="utf-8") as file:
            json.dump(backup, file, ensure_ascii=False, indent=2)
            file.write("\n")
    except Exception:
        shutil.rmtree(args.output)
        raise

    print(f"Exported {len(rows)} published articles and {len(image_names)} images to {args.output}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, sqlite3.Error, ValueError) as error:
        print(f"Article backup failed: {error}", file=sys.stderr)
        sys.exit(1)
