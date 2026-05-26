#!/usr/bin/env python3
import json
import pathlib


ROOT = pathlib.Path(__file__).resolve().parents[1]
DOCS_DIR = ROOT / "docs"

BEARER_SECURITY_DEFINITION = {
    "type": "apiKey",
    "name": "Authorization",
    "in": "header",
    "description": "Enter the token with the Bearer prefix, e.g. Bearer eyJ...",
}


def patch_swagger_json():
    path = DOCS_DIR / "swagger.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    data["securityDefinitions"] = {"BearerAuth": BEARER_SECURITY_DEFINITION}
    data["security"] = [{"BearerAuth": []}]
    login = data.get("paths", {}).get("/auth/login", {}).get("post")
    if isinstance(login, dict):
        login["security"] = []
    path.write_text(json.dumps(data, ensure_ascii=False, indent=4) + "\n", encoding="utf-8")


def patch_docs_go():
    path = DOCS_DIR / "docs.go"
    content = path.read_text(encoding="utf-8")

    base_marker = '    "basePath": "{{.BasePath}}",\n'
    security_block = (
        '    "security": [\n'
        '        {\n'
        '            "BearerAuth": []\n'
        '        }\n'
        '    ],\n'
    )
    if security_block not in content:
        content = content.replace(base_marker, base_marker + security_block, 1)

    login_marker = '        "/auth/login": {\n            "post": {\n'
    login_security = '                "security": [],\n'
    if login_marker in content and login_marker + login_security not in content:
        content = content.replace(login_marker, login_marker + login_security, 1)

    security_def_start = content.find('    "securityDefinitions": {')
    external_docs_start = content.find('    "externalDocs": {')
    if security_def_start != -1 and external_docs_start != -1 and security_def_start < external_docs_start:
        replacement = (
            '    "securityDefinitions": {\n'
            '        "BearerAuth": {\n'
            '            "type": "apiKey",\n'
            '            "name": "Authorization",\n'
            '            "in": "header",\n'
            '            "description": "Enter the token with the Bearer prefix, e.g. Bearer eyJ..."\n'
            '        }\n'
            '    },\n'
        )
        content = content[:security_def_start] + replacement + content[external_docs_start:]

    path.write_text(content, encoding="utf-8")


def patch_swagger_yaml():
    path = DOCS_DIR / "swagger.yaml"
    content = path.read_text(encoding="utf-8")

    login_marker = "  /auth/login:\n    post:\n"
    login_security = "      security: []\n"
    if login_marker in content and login_marker + login_security not in content:
        content = content.replace(login_marker, login_marker + login_security, 1)

    if "\nsecurity:\n- BearerAuth: []\n" not in content:
        marker = "securityDefinitions:\n"
        content = content.replace(marker, "security:\n- BearerAuth: []\n" + marker, 1)

    security_def_start = content.find("securityDefinitions:\n")
    swagger_start = content.find("swagger: \"2.0\"")
    if security_def_start != -1 and swagger_start != -1 and security_def_start < swagger_start:
        replacement = (
            "securityDefinitions:\n"
            "  BearerAuth:\n"
            "    description: Enter the token with the Bearer prefix, e.g. Bearer eyJ...\n"
            "    in: header\n"
            "    name: Authorization\n"
            "    type: apiKey\n"
        )
        content = content[:security_def_start] + replacement + content[swagger_start:]

    path.write_text(content, encoding="utf-8")


def main():
    patch_swagger_json()
    patch_docs_go()
    patch_swagger_yaml()


if __name__ == "__main__":
    main()
