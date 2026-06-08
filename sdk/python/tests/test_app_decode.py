from __future__ import annotations

import pathlib
import unittest

from gestalt import Response
from gestalt._app_decode import InvokeError, decode_app_result, decode_graphql_result

FIXTURE_ROOT = pathlib.Path(__file__).resolve().parents[2] / "testdata" / "app_invoke"


def fixture(name: str) -> bytes:
    return (FIXTURE_ROOT / name).read_bytes()


class AppDecodeTests(unittest.TestCase):
    def result(self, name: str, status: int = 200) -> Response[bytes]:
        return Response(status=status, headers={}, body=fixture(name))

    def test_app_decode_fixtures(self) -> None:
        self.assertEqual(
            decode_app_result("github", "get_issue", self.result("success_envelope.json")),
            {"id": 1},
        )
        self.assertEqual(
            decode_app_result("github", "get_issue", self.result("plain_ok.json")),
            {"pull_request": {"id": 123, "title": "Fix transport"}},
        )
        self.assertEqual(
            decode_app_result("github", "get_issue", self.result("empty_body.json")),
            {},
        )
        self.assertEqual(
            decode_app_result("github", "get_issue", self.result("success_missing_data.json")),
            {"status": "success", "ok": True},
        )
        self.assertIsNone(
            decode_app_result("github", "get_issue", self.result("success_null_data.json"))
        )
        self.assertEqual(
            decode_app_result("github", "get_issue", self.result("unknown_status.json")),
            {"status": "pending", "data": {"id": 2}},
        )
        self.assertEqual(
            decode_app_result("github", "get_issue", self.result("non_string_status.json")),
            {"status": True, "data": {"id": 3}},
        )
        self.assertEqual(
            decode_app_result("github", "get_issue", self.result("array_ok.json")),
            [1, 2, 3],
        )
        self.assertEqual(
            decode_app_result("github", "get_issue", self.result("primitive_ok.json")),
            "ok",
        )

    def test_app_decode_errors(self) -> None:
        with self.assertRaises(InvokeError) as envelope:
            decode_app_result("github", "get_issue", self.result("error_envelope.json"))
        self.assertEqual(envelope.exception.code, "missing_credential")
        self.assertEqual(str(envelope.exception), "missing credential")

        with self.assertRaises(InvokeError) as http_error:
            decode_app_result("github", "get_issue", self.result("http_401.json", 401))
        self.assertEqual(http_error.exception.status, 401)
        self.assertTrue(http_error.exception.raw_body)

        with self.assertRaises(InvokeError):
            decode_app_result("github", "get_issue", self.result("invalid_json.txt"))

    def test_response_helpers(self) -> None:
        raw = self.result("success_envelope.json")
        self.assertTrue(raw.ok)
        self.assertEqual(raw.bytes(), fixture("success_envelope.json"))
        self.assertEqual(raw.text(), fixture("success_envelope.json").decode("utf-8"))
        self.assertEqual(raw.decode_json(), {"status": "success", "data": {"id": 1}})
        self.assertIs(raw.raise_for_status(), raw)

        failed = Response(status=503, headers={}, body=b"not json")
        self.assertFalse(failed.ok)
        with self.assertRaises(InvokeError):
            failed.raise_for_status()

    def test_graphql_errors(self) -> None:
        self.assertEqual(
            decode_graphql_result("linear", self.result("graphql_ok.json")),
            {"data": {"viewer": {"id": "user-1"}}, "errors": []},
        )
        self.assertEqual(
            decode_graphql_result("linear", self.result("graphql_malformed_errors.json")),
            {"data": {"viewer": None}, "errors": {"message": "not an array"}},
        )
        with self.assertRaises(InvokeError) as error:
            decode_graphql_result("linear", self.result("graphql_errors.json"))
        self.assertEqual(error.exception.code, "graphql_errors")
        with self.assertRaises(InvokeError) as envelope_error:
            decode_graphql_result("linear", self.result("graphql_success_envelope_errors.json"))
        self.assertEqual(envelope_error.exception.code, "graphql_errors")


if __name__ == "__main__":
    unittest.main()
