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
            self.assertLess(body.index("/static/login.js"), body.index("https://accounts.google.com/gsi/client"))

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


class ArticlesNewsletterTest(unittest.TestCase):
    def test_homepage_renders_newsletter_and_updated_journal_copy(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            app_module = load_app(
                MCP_ARTICLES_BASE_URL="http://localhost:8080",
                MCP_ARTICLES_SECRET_KEY="test-secret",
                MCP_ARTICLES_DB_PATH=os.path.join(tmp, "articles.db"),
            )
            client = app_module.app.test_client()

            response = client.get("/")

            self.assertEqual(response.status_code, 200)
            body = response.get_data(as_text=True)
            self.assertIn("my understanding of the platform topics", body)
            self.assertIn('action="/newsletter/subscribe"', body)

    def test_article_detail_renders_newsletter_form(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            app_module = load_app(
                MCP_ARTICLES_BASE_URL="http://localhost:8080",
                MCP_ARTICLES_SECRET_KEY="test-secret",
                MCP_ARTICLES_DB_PATH=os.path.join(tmp, "articles.db"),
            )
            client = app_module.app.test_client()

            response = client.get("/mcp/request-flow/")

            self.assertEqual(response.status_code, 200)
            body = response.get_data(as_text=True)
            self.assertIn('id="newsletter"', body)
            self.assertIn('action="/newsletter/subscribe"', body)
            self.assertIn('name="next" value="/mcp/request-flow/"', body)

    def test_newsletter_subscribe_export_and_unsubscribe(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            app_module = load_app(
                MCP_ARTICLES_BASE_URL="http://localhost:8080",
                MCP_ARTICLES_SECRET_KEY="test-secret",
                MCP_ARTICLES_NEWSLETTER_EXPORT_TOKEN="export-token",
                MCP_ARTICLES_DB_PATH=os.path.join(tmp, "articles.db"),
            )
            client = app_module.app.test_client()

            with client.session_transaction() as session:
                session["csrf_token"] = "csrf"

            response = client.post(
                "/newsletter/subscribe",
                data={"csrf_token": "csrf", "email": " Reader@Example.COM ", "next": "/"},
            )

            self.assertEqual(response.status_code, 302)
            subscribers = app_module.active_newsletter_subscribers()
            self.assertEqual(len(subscribers), 1)
            self.assertEqual(subscribers[0].email, "reader@example.com")

            export_response = client.get("/newsletter/export.csv?token=export-token")
            self.assertEqual(export_response.status_code, 200)
            csv_body = export_response.get_data(as_text=True)
            self.assertIn("reader@example.com", csv_body)
            self.assertIn("/newsletter/unsubscribe/", csv_body)

            unsubscribe_response = client.get(f"/newsletter/unsubscribe/{subscribers[0].unsubscribe_token}")
            self.assertEqual(unsubscribe_response.status_code, 302)
            self.assertEqual(app_module.active_newsletter_subscribers(), ())

    def test_newsletter_export_requires_token(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            app_module = load_app(
                MCP_ARTICLES_BASE_URL="http://localhost:8080",
                MCP_ARTICLES_SECRET_KEY="test-secret",
                MCP_ARTICLES_NEWSLETTER_EXPORT_TOKEN="export-token",
                MCP_ARTICLES_DB_PATH=os.path.join(tmp, "articles.db"),
            )
            client = app_module.app.test_client()

            self.assertEqual(client.get("/newsletter/export.csv").status_code, 403)
            self.assertEqual(client.get("/newsletter/export.csv?token=wrong").status_code, 403)


if __name__ == "__main__":
    unittest.main()
