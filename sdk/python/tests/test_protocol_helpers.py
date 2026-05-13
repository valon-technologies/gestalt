import unittest

import gestalt
from gestalt import testing


class ProtocolHelperTests(unittest.TestCase):
    def test_testing_exports_native_fixture_helpers(self) -> None:
        message_dict = testing.agent_message_to_dict(
            gestalt.AgentMessage(role="user", text="hi")
        )

        self.assertEqual(message_dict["role"], "user")

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
            "AuthorizationResource",
        ):
            with self.subTest(name=name):
                with self.assertRaises(ImportError):
                    exec(f"from gestalt import {name}", {})


if __name__ == "__main__":
    unittest.main()
