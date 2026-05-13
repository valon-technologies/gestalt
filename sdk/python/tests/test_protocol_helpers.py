import unittest

import gestalt
from gestalt import protocol, testing
from gestalt.protocol import v1 as protocol_v1


class ProtocolHelperTests(unittest.TestCase):
    def test_protocol_exports_well_known_helpers(self) -> None:
        message = protocol.struct_from_dict({"b": 2, "a": 1})
        self.assertEqual(protocol.struct_to_dict(message), {"a": 1.0, "b": 2.0})
        self.assertEqual(
            protocol.value_to_json(protocol.value_from_json(["x", 1])),
            ["x", 1.0],
        )

    def test_protocol_exports_well_known_type_aliases(self) -> None:
        self.assertIsInstance(protocol.Timestamp(), protocol.Timestamp)
        self.assertIsInstance(protocol.Value(), protocol.Value)

    def test_protocol_v1_stays_module_based(self) -> None:
        request = protocol_v1.workflow_pb2.StartWorkflowProviderRunRequest(
            workflow_key="sync",
        )

        self.assertEqual(request.workflow_key, "sync")
        self.assertFalse(hasattr(protocol_v1, "StartWorkflowProviderRunRequest"))

    def test_testing_exports_proto_fixture_helpers(self) -> None:
        proto_dict = testing.agent_message_to_proto_dict(
            gestalt.AgentMessage(role="user", text="hi")
        )

        self.assertEqual(proto_dict["role"], "user")

    def test_root_low_level_imports_fail(self) -> None:
        for name in (
            "struct_from_dict",
            "agent_message_to_proto_dict",
            "indexeddb_record_to_proto",
            "AuthorizationResource",
        ):
            with self.subTest(name=name):
                with self.assertRaises(ImportError):
                    exec(f"from gestalt import {name}", {})


if __name__ == "__main__":
    unittest.main()
