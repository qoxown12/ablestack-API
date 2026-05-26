#!/usr/bin/env python3
import json
import os
import shutil
import sys
import tempfile


def merge_missing(current, defaults):
    changed = False
    if not isinstance(current, dict) or not isinstance(defaults, dict):
        return current, changed

    for key, default_value in defaults.items():
        if key not in current:
            current[key] = default_value
            changed = True
            continue

        current_value = current[key]
        if isinstance(current_value, dict) and isinstance(default_value, dict):
            merged, nested_changed = merge_missing(current_value, default_value)
            current[key] = merged
            changed = changed or nested_changed

    return current, changed


def read_json(path):
    with open(path, "r", encoding="utf-8") as handle:
        return json.load(handle)


def write_json_atomic(path, data):
    directory = os.path.dirname(path) or "."
    stat_info = os.stat(path)
    fd, tmp_path = tempfile.mkstemp(prefix=".merge-json-", dir=directory, text=True)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(data, handle, ensure_ascii=False, indent=4)
            handle.write("\n")
        os.chmod(tmp_path, stat_info.st_mode & 0o7777)
        try:
            os.chown(tmp_path, stat_info.st_uid, stat_info.st_gid)
        except PermissionError:
            pass
        os.replace(tmp_path, path)
    except Exception:
        try:
            os.unlink(tmp_path)
        except FileNotFoundError:
            pass
        raise


def main():
    if len(sys.argv) != 3:
        print("usage: merge-json-defaults.py <target-json> <defaults-json>", file=sys.stderr)
        return 2

    target_path = sys.argv[1]
    defaults_path = sys.argv[2]

    if not os.path.exists(target_path) or not os.path.exists(defaults_path):
        return 0

    if os.path.getsize(target_path) == 0:
        backup_path = target_path + ".rpmsave-before-merge"
        if not os.path.exists(backup_path):
            shutil.copy2(target_path, backup_path)
        defaults = read_json(defaults_path)
        write_json_atomic(target_path, defaults)
        return 0

    try:
        current = read_json(target_path)
        defaults = read_json(defaults_path)
    except Exception as exc:
        print(f"skip json merge: {exc}", file=sys.stderr)
        return 0

    merged, changed = merge_missing(current, defaults)
    if changed:
        backup_path = target_path + ".rpmsave-before-merge"
        if not os.path.exists(backup_path):
            shutil.copy2(target_path, backup_path)
        write_json_atomic(target_path, merged)

    return 0


if __name__ == "__main__":
    sys.exit(main())
