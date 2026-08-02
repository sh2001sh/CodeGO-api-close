#!/usr/bin/env python3
"""Grant one or more blind boxes to users listed by external ID.

The input file contains one six-character public user ID per line. The script
defaults to a dry run. Use --apply only after confirming the preview.
"""

from __future__ import annotations

import argparse
import http.cookiejar
import json
import re
import sys
from dataclasses import asdict, dataclass
from datetime import UTC, datetime
from getpass import getpass
from pathlib import Path
from typing import Any, Iterable
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urljoin
from urllib.request import HTTPCookieProcessor, Request, build_opener


EXTERNAL_ID_PATTERN = re.compile(r"^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{6}$")


@dataclass
class GrantResult:
    external_id: str
    status: str
    user_id: int | None = None
    grant_id: int | None = None
    trade_no: str | None = None
    detail: str = ""


class ApiClient:
    def __init__(self, base_url: str, cookie_header: str = "") -> None:
        self.base_url = base_url.rstrip("/") + "/"
        self.cookies = http.cookiejar.CookieJar()
        self.opener = build_opener(HTTPCookieProcessor(self.cookies))
        self.cookie_header = cookie_header.strip()

    def request(
        self,
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        query: dict[str, str] | None = None,
    ) -> dict[str, Any]:
        url = urljoin(self.base_url, path.lstrip("/"))
        if query:
            url = f"{url}?{urlencode(query)}"
        body = None
        headers = {"Accept": "application/json"}
        if self.cookie_header:
            headers["Cookie"] = self.cookie_header
        if payload is not None:
            body = json.dumps(payload).encode("utf-8")
            headers["Content-Type"] = "application/json"
        request = Request(url, data=body, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=30) as response:
                raw = response.read().decode("utf-8")
        except HTTPError as error:
            raw = error.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"HTTP {error.code}: {raw}") from error
        except URLError as error:
            raise RuntimeError(f"network error: {error.reason}") from error

        try:
            data = json.loads(raw)
        except json.JSONDecodeError as error:
            raise RuntimeError(f"non-JSON response: {raw[:200]}") from error
        if not isinstance(data, dict):
            raise RuntimeError("unexpected API response shape")
        return data

    def login(self, username: str, password: str) -> None:
        response = self.request(
            "POST",
            "/api/user/login",
            {"username": username, "password": password},
        )
        if not response.get("success"):
            raise RuntimeError(response.get("message") or "admin login failed")
        if (response.get("data") or {}).get("require_2fa"):
            raise RuntimeError("admin account requires 2FA; use --admin-cookie instead")


def load_external_ids(path: Path) -> list[str]:
    ids: list[str] = []
    seen: set[str] = set()
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        value = line.strip().upper()
        if not value or value.startswith("#"):
            continue
        if not EXTERNAL_ID_PATTERN.fullmatch(value):
            raise ValueError(f"line {line_number}: invalid external ID {value!r}")
        if value not in seen:
            ids.append(value)
            seen.add(value)
    if not ids:
        raise ValueError("no valid external IDs found")
    return ids


def iter_records(value: Any) -> Iterable[dict[str, Any]]:
    if isinstance(value, dict):
        if "id" in value and ("external_id" in value or "externalId" in value):
            yield value
        for child in value.values():
            yield from iter_records(child)
    elif isinstance(value, list):
        for child in value:
            yield from iter_records(child)


def find_user_id(client: ApiClient, external_id: str) -> int | None:
    response = client.request("GET", "/api/user/search", query={"keyword": external_id})
    if not response.get("success"):
        raise RuntimeError(response.get("message") or "user search failed")

    matches = {
        int(record["id"])
        for record in iter_records(response.get("data"))
        if str(record.get("external_id") or record.get("externalId") or "").upper() == external_id
    }
    if len(matches) > 1:
        raise RuntimeError(f"multiple users matched external ID {external_id}")
    return next(iter(matches), None)


def grant_blind_box(
    client: ApiClient,
    user_id: int,
    external_id: str,
    quantity: int,
    reason: str,
    batch: str,
) -> GrantResult:
    idempotency_key = f"blind-box-grant:{batch}:{external_id}"
    response = client.request(
        "POST",
        f"/api/blind-box/admin/users/{user_id}/grants",
        {
            "quantity": quantity,
            "reason": reason,
            "idempotency_key": idempotency_key,
        },
    )
    if not response.get("success"):
        return GrantResult(
            external_id=external_id,
            status="failed",
            user_id=user_id,
            detail=str(response.get("message") or "grant failed"),
        )

    data = response.get("data") or {}
    grant = data.get("grant") or {}
    order = data.get("order") or {}
    return GrantResult(
        external_id=external_id,
        status="granted",
        user_id=user_id,
        grant_id=grant.get("id"),
        trade_no=order.get("trade_no"),
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ids-file", required=True, type=Path, help="TXT file with one external ID per line")
    parser.add_argument("--base-url", default="https://shu26.cfd", help="Code Go site URL")
    parser.add_argument("--quantity", type=int, default=1, help="Blind boxes per user (default: 1)")
    parser.add_argument("--reason", default="LinuxDO community blind box grant", help="Audit reason recorded for every grant")
    parser.add_argument("--batch", default=f"manual-{datetime.now(UTC):%Y%m%d}", help="Stable idempotency batch name")
    parser.add_argument("--admin-cookie", default="", help="Admin browser Cookie header; prefer a short-lived session")
    parser.add_argument("--admin-username", default="", help="Prompts for the password and creates an admin session")
    parser.add_argument("--apply", action="store_true", help="Actually grant blind boxes; otherwise only preview")
    parser.add_argument("--output", type=Path, help="Write detailed JSON results to this path")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if not args.ids_file.is_file():
        print(f"IDs file not found: {args.ids_file}", file=sys.stderr)
        return 2
    if args.quantity <= 0 or args.quantity > 1000:
        print("--quantity must be between 1 and 1000", file=sys.stderr)
        return 2
    if not args.admin_cookie and not args.admin_username:
        print("provide --admin-cookie or --admin-username", file=sys.stderr)
        return 2

    try:
        external_ids = load_external_ids(args.ids_file)
    except ValueError as error:
        print(error, file=sys.stderr)
        return 2

    client = ApiClient(args.base_url, args.admin_cookie)
    if args.admin_username:
        try:
            client.login(args.admin_username, getpass("Admin password: "))
        except RuntimeError as error:
            print(f"login failed: {error}", file=sys.stderr)
            return 1

    results: list[GrantResult] = []
    for external_id in external_ids:
        try:
            user_id = find_user_id(client, external_id)
            if user_id is None:
                results.append(GrantResult(external_id, "not_found", detail="no exact external ID match"))
            elif args.apply:
                results.append(grant_blind_box(client, user_id, external_id, args.quantity, args.reason, args.batch))
            else:
                results.append(GrantResult(external_id, "ready", user_id=user_id))
        except RuntimeError as error:
            results.append(GrantResult(external_id, "failed", detail=str(error)))

    for result in results:
        print(f"{result.external_id}\t{result.status}\tuser={result.user_id or '-'}\t{result.detail}")

    output = args.output or args.ids_file.with_name(f"{args.ids_file.stem}-blind-box-results.json")
    output.write_text(json.dumps([asdict(result) for result in results], ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"results: {output}")

    failures = [result for result in results if result.status in {"failed", "not_found"}]
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
