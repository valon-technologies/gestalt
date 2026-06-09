import unittest

from gestalt import authorization


class AuthorizationConversionTest(unittest.TestCase):
    def test_check_access_request_preserves_absent_properties(self) -> None:
        request = authorization.CheckAccessRequest(
            subject=authorization.Subject(type="user", id="u1"),
            action=authorization.Action(name="read"),
            resource=authorization.Resource(type="document", id="d1"),
        )

        wire = authorization.check_access_request_to_proto(request)

        self.assertFalse(wire.subject.HasField("properties"))
        self.assertFalse(wire.action.HasField("properties"))
        self.assertFalse(wire.resource.HasField("properties"))

        native = authorization.check_access_request_from_proto(wire)
        subject = native.subject
        action = native.action
        resource = native.resource
        assert subject is not None
        assert action is not None
        assert resource is not None
        self.assertIsNone(subject.properties)
        self.assertIsNone(action.properties)
        self.assertIsNone(resource.properties)

    def test_authorization_model_conversion_uses_generated_proto_names(self) -> None:
        wire = authorization.authorization_model_to_proto(authorization.Model(id="m1"))

        self.assertIsInstance(wire, authorization.pb.AuthorizationModel)
        self.assertEqual(wire.id, "m1")

    def test_active_model_resource_type_filter_uses_generated_proto_name(self) -> None:
        wire = authorization.list_active_model_resource_types_request_to_proto(
            authorization.ListActiveModelResourceTypesRequest(
                filter=authorization.ModelResourceTypeFilter(
                    name="document",
                    source_layer=authorization.SOURCE_LAYER_RUNTIME,
                )
            )
        )

        self.assertIsInstance(
            wire.filter,
            authorization.pb.AuthorizationModelResourceTypeFilter,
        )
        self.assertEqual(wire.filter.name, "document")

    def test_oneof_factories_and_unset_round_trip(self) -> None:
        target = authorization.RelationshipTarget.from_subject(
            authorization.Subject(type="user", id="u1")
        )
        target_wire = authorization.relationship_target_to_proto(target)
        target_native = authorization.relationship_target_from_proto(target_wire)
        subject = target_native.subject
        assert subject is not None
        self.assertEqual(subject.id, "u1")

        unset_target = authorization.relationship_target_from_proto(
            authorization.pb.RelationshipTarget()
        )
        self.assertIsNone(unset_target.subject)
        self.assertIsNone(unset_target.resource)
        self.assertIsNone(unset_target.subject_set)

        allowed = authorization.ModelAllowedTarget.from_subject_type("user")
        allowed_wire = authorization.model_allowed_target_to_proto(allowed)
        allowed_native = authorization.model_allowed_target_from_proto(allowed_wire)
        self.assertEqual(allowed_native.subject_type, "user")

        unset_allowed = authorization.model_allowed_target_from_proto(
            authorization.pb.ModelAllowedTarget()
        )
        self.assertIsNone(unset_allowed.subject_type)
        self.assertIsNone(unset_allowed.resource_type)
        self.assertIsNone(unset_allowed.subject_set_type)

        target.resource = authorization.Resource(type="document", id="d1")

        with self.assertRaisesRegex(ValueError, "RelationshipTarget accepts exactly one variant"):
            authorization.relationship_target_to_proto(target)

        allowed.resource_type = "document"

        with self.assertRaisesRegex(ValueError, "ModelAllowedTarget accepts exactly one variant"):
            authorization.model_allowed_target_to_proto(allowed)

    def test_closed_client_rejects_rpc_before_using_transport(self) -> None:
        client = object.__new__(authorization.Client)
        client._closed = True

        with self.assertRaisesRegex(RuntimeError, "authorization: client is closed"):
            client.check_access(authorization.CheckAccessRequest())


if __name__ == "__main__":
    unittest.main()
