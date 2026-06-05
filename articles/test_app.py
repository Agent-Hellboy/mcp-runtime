from __future__ import annotations

import importlib
import os
import sys
import tempfile
import unittest
from unittest import mock


def load_app(**env: str):
    sys.modules.pop("app", None)
    old_env = os.environ.copy()
    os.environ.update(env)
    try:
        sys.path.insert(0, os.path.dirname(__file__))
        return importlib.import_module("app")
    finally:
        sys.path.remove(os.path.dirname(__file__))
        os.environ.clear()
        os.environ.update(old_env)


class ArticlesGoogleLoginTest(unittest.TestCase):
    def test_login_renders_google_button_with_client_id_only(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            app_module = load_app(
                MCP_ARTICLES_BASE_URL="http://localhost:8080",
                MCP_ARTICLES_SECRET_KEY="test-secret",
                MCP_ARTICLES_GOOGLE_CLIENT_ID="client.apps.googleusercontent.com",
                MCP_ARTICLES_DB_PATH=os.path.join(tmp, "articles.db"),
            )
            client = app_module.app.test_client()

            response = client.get("/login?next=/mcp/request-flow/")

            self.assertEqual(response.status_code, 200)
            body = response.get_data(as_text=True)
            self.assertIn("https://accounts.google.com/gsi/client", body)
            self.assertIn("client.apps.googleusercontent.com", body)

    def test_google_token_login_accepts_valid_id_token(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            app_module = load_app(
                MCP_ARTICLES_BASE_URL="http://localhost:8080",
                MCP_ARTICLES_SECRET_KEY="test-secret",
                MCP_ARTICLES_GOOGLE_CLIENT_ID="client.apps.googleusercontent.com",
                MCP_ARTICLES_DB_PATH=os.path.join(tmp, "articles.db"),
            )
            client = app_module.app.test_client()

            with client.session_transaction() as session:
                session["csrf_token"] = "csrf"
                session["login_next"] = "/mcp/request-flow/"

            tokeninfo = {
                "aud": "client.apps.googleusercontent.com",
                "iss": "https://accounts.google.com",
                "sub": "google-user",
                "email": "reader@example.com",
                "email_verified": "true",
                "name": "Reader",
            }
            mocked_response = mock.Mock(status_code=200)
            mocked_response.json.return_value = tokeninfo
            with mock.patch.object(app_module.requests, "get", return_value=mocked_response):
                response = client.post(
                    "/auth/google/token",
                    data={"csrf_token": "csrf", "credential": "id-token"},
                )

            self.assertEqual(response.status_code, 302)
            self.assertEqual(response.headers["Location"], "/mcp/request-flow/")
            with client.session_transaction() as session:
                self.assertEqual(session["user"]["email"], "reader@example.com")


if __name__ == "__main__":
    unittest.main()
