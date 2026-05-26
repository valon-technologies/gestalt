import unittest
from typing import get_type_hints

import gestalt
from gestalt import testing


class ProtocolHelperTests(unittest.TestCase):
    def test_testing_exports_native_fixture_helpers(self) -> None:
        message_dict = testing.agent_message_to_dict(
            gestalt.AgentMessage(role="user", text="hi")
        )

        self.assertEqual(message_dict["role"], "user")

    def test_authorization_provider_methods_are_annotated_with_native_types(
        self,
    ) -> None:
        evaluate_hints = get_type_hints(gestalt.AuthorizationProvider.evaluate)
        search_subjects_hints = get_type_hints(
            gestalt.AuthorizationProvider.search_subjects
        )

        self.assertIs(evaluate_hints["request"], gestalt.AccessEvaluationRequest)
        self.assertIs(evaluate_hints["return"], gestalt.AccessDecision)
        self.assertIs(search_subjects_hints["request"], gestalt.SubjectSearchRequest)
        self.assertIs(search_subjects_hints["return"], gestalt.SubjectSearchResponse)

    def test_public_low_level_imports_fail(self) -> None:
        with self.assertRaises(ImportError):
            exec("from gestalt import protocol", {})
        with self.assertRaises(ModuleNotFoundError):
            __import__("gestalt.protocol")
        with self.assertRaises(ModuleNotFoundError):
            __import__("gestalt.protocol.v1")
        for name in (
            "struct_from_dict",
            "indexeddb_record_to_proto",
        ):
            with self.subTest(name=name):
                with self.assertRaises(ImportError):
                    exec(f"from gestalt import {name}", {})


if __name__ == "__main__":
    unittest.main()
