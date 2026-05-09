import unittest

import gestalt


class ProtocolHelperTests(unittest.TestCase):
    def test_well_known_helpers_round_trip_native_values(self) -> None:
        message = gestalt.struct_from_dict({"b": 2, "a": 1})
        self.assertEqual(gestalt.struct_to_dict(message), {"a": 1.0, "b": 2.0})
        self.assertEqual(gestalt.value_to_json(gestalt.value_from_json(["x", 1])), ["x", 1.0])

    def test_well_known_type_aliases_are_public(self) -> None:
        self.assertIsInstance(gestalt.Timestamp(), gestalt.Timestamp)
        self.assertIsInstance(gestalt.Value(), gestalt.Value)


if __name__ == "__main__":
    unittest.main()
