import unittest

import gestalt


class ProtocolHelperTests(unittest.TestCase):
    def test_proto_boundary_helpers_round_trip_binary_and_json(self) -> None:
        message = gestalt.struct_from_dict({"b": 2, "a": 1})

        first = gestalt.marshal_proto_deterministic(message)
        second = gestalt.marshal_proto_deterministic(message)

        self.assertEqual(first, second)

        decoded = gestalt.unmarshal_proto(first, gestalt.Struct())
        self.assertEqual(gestalt.struct_to_dict(decoded), {"a": 1.0, "b": 2.0})

        json_data = gestalt.marshal_proto_json(message, sort_keys=True)
        from_json = gestalt.unmarshal_proto_json(json_data, gestalt.Struct())
        self.assertEqual(gestalt.struct_to_dict(from_json), {"a": 1.0, "b": 2.0})

    def test_well_known_type_aliases_are_public(self) -> None:
        self.assertIsInstance(gestalt.Empty(), gestalt.ProtoMessage)
        self.assertIsInstance(gestalt.Timestamp(), gestalt.ProtoMessage)
        self.assertIsInstance(gestalt.Value(), gestalt.ProtoMessage)


if __name__ == "__main__":
    unittest.main()
