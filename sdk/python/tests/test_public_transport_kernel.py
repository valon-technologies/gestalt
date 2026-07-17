"""Direct tests for the generated public REST transport kernel."""

from __future__ import annotations

import base64
import json
import unittest
from dataclasses import replace
from pathlib import Path

from gestalt._gen.v1 import app_pb2
from gestalt.public.generated.metadata import METHOD_APP_INVOKE, Method, PublicField
from gestalt.public.generated.transport_kernel import (
    RawRestResponse,
    build_rest_body,
    build_rest_path,
    build_rest_query,
    decode_rest_response,
    parse_gateway_error,
)
from gestalt.rpc_support import GestaltError, GestaltErrorCode

_FIXTURES = Path(__file__).resolve().parents[2] / "testdata/public_conformance/transport_kernel_cases.json"


class PublicTransportKernelTests(unittest.TestCase):
  @classmethod
  def setUpClass(cls) -> None:
    cls.cases = json.loads(_FIXTURES.read_text())

  def test_gateway_pascal_case(self) -> None:
    err = parse_gateway_error(
      401,
      b'{"error":"missing authorization","code":"Unauthenticated"}',
    )
    self.assertEqual(err.code, GestaltErrorCode.UNAUTHENTICATED)
    self.assertEqual(err.message, "missing authorization")

  def test_compatibility_reexports(self) -> None:
    from gestalt.public import errors, rest_mapping

    self.assertIs(errors.parse_gateway_error, parse_gateway_error)
    self.assertEqual(
      rest_mapping.encode_query_string([("a", "1"), ("b", "2")]),
      "a=1&b=2",
    )
    with self.assertRaises(ValueError):
      rest_mapping.build_rest_path(
        _method_for_prepare_case(
          {
            "overrideQueryFields": [],
            "request": {"app": "example", "operation": "sync"},
          }
        ),
        {"operation": "sync"},
      )
    with self.assertRaises(ValueError):
      rest_mapping.build_rest_query(
        _method_for_prepare_case(
          {
            "overrideQueryFields": [
              {"name": "tags", "jsonName": "tags"},
            ],
            "request": {
              "app": "example",
              "operation": "sync",
              "tags": [{"bad": "object"}],
            },
          }
        ),
        {"tags": [{"bad": "object"}]},
      )

  def test_fixture_cases_are_covered(self) -> None:
    for case in self.cases:
      if not any(
        case.get(key)
        for key in (
          "expectPrepare",
          "expectDecode",
          "expectGatewayError",
          "expectGestaltError",
          "expectPrepareError",
        )
      ):
        self.fail(f"case {case['id']} has no expectations")

  def test_prepare_cases_from_fixture(self) -> None:
    for case in self.cases:
      expect = case.get("expectPrepare")
      request = case.get("request")
      if not expect or not request:
        continue
      method = _method_for_prepare_case(case)
      path = build_rest_path(method, request)
      query = tuple(build_rest_query(method, request))
      body = build_rest_body(method, request)
      self.assertEqual(method.http_verb, expect["verb"], case["id"])
      self.assertEqual(path, expect["path"], case["id"])
      self.assertEqual(list(query), [tuple(pair) for pair in expect["query"]], case["id"])
      if expect["body"] is None:
        self.assertIsNone(body, case["id"])
      else:
        self.assertEqual(body, expect["body"], case["id"])

  def test_prepare_error_cases_from_fixture(self) -> None:
    for case in self.cases:
      expect = case.get("expectPrepareError")
      request = case.get("request")
      if not expect or not request:
        continue
      method = _method_for_prepare_case(case)
      with self.assertRaises(GestaltError) as ctx:
        build_rest_query(method, request)
      self.assertEqual(ctx.exception.code, expect["code"], case["id"])

  def test_decode_cases_from_fixture(self) -> None:
    for case in self.cases:
      expect = case.get("expectDecode")
      raw = case.get("rawResponse")
      if not expect or not raw:
        continue
      response = decode_rest_response(
        METHOD_APP_INVOKE,
        app_pb2.OperationResult,
        _raw_response_from_fixture(raw),
      )
      self.assertEqual(response.status, expect["status"], case["id"])
      want_body = base64.b64decode(expect["bodyBase64"])
      self.assertEqual(response.body, want_body, case["id"])
      for key in expect.get("headerKeys", []):
        self.assertIn(key, response.headers, case["id"])
      for key, want_count in expect.get("headerValueCounts", {}).items():
        self.assertEqual(len(response.headers[key].values), want_count, case["id"])

  def test_gateway_cases_from_fixture(self) -> None:
    for case in self.cases:
      expect = case.get("expectGatewayError")
      raw = case.get("rawResponse")
      if not expect or not raw:
        continue
      body = raw.get("bodyText", "").encode()
      err = parse_gateway_error(raw["status"], body)
      self.assertEqual(err.code, expect["code"], case["id"])
      if "message" in expect:
        self.assertEqual(err.message, expect["message"], case["id"])

  def test_gestalt_error_cases_from_fixture(self) -> None:
    for case in self.cases:
      expect = case.get("expectGestaltError")
      raw = case.get("rawResponse")
      if not expect or not raw:
        continue
      with self.assertRaises(GestaltError) as ctx:
        decode_rest_response(
          METHOD_APP_INVOKE,
          app_pb2.OperationResult,
          _raw_response_from_fixture(raw),
        )
      self.assertEqual(ctx.exception.code, expect["code"], case["id"])


def _method_for_prepare_case(case: dict) -> Method:
  method = METHOD_APP_INVOKE
  override_query = case.get("overrideQueryFields")
  if override_query:
    extra = tuple(
      PublicField(name=field["name"], json_name=field["jsonName"])
      for field in override_query
    )
    method = replace(method, http_query_fields=method.http_query_fields + extra)
  if "overrideHttpBody" in case:
    method = replace(method, http_body=case["overrideHttpBody"])
  return method


def _raw_response_from_fixture(raw: dict) -> RawRestResponse:
  if "bodyText" in raw:
    body = raw["bodyText"].encode()
  elif "bodyBase64" in raw:
    body = base64.b64decode(raw["bodyBase64"])
  else:
    body = b""
  headers = tuple(tuple(pair) for pair in raw.get("headers", []))
  return RawRestResponse(status=raw["status"], headers=headers, body=body)


if __name__ == "__main__":
  unittest.main()
