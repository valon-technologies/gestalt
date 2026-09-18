import pytest

from gestalt._catalog import (
    Catalog,
    CatalogOperation,
    catalog_to_dict,
    catalog_to_proto,
)


def test_catalog_api_exposure_preserves_legacy_and_browser_modes() -> None:
    catalog = Catalog(
        operations=[
            CatalogOperation(id="public", method="GET", api=True),
            CatalogOperation(id="private", method="GET", api=False),
            CatalogOperation(id="browser", method="GET", api="browserSession"),
        ]
    )

    assert catalog_to_dict(catalog, field_style="json")["operations"] == [
        {"id": "public", "method": "GET", "api": True},
        {"id": "private", "method": "GET", "api": False},
        {"id": "browser", "method": "GET", "api": "browserSession"},
    ]


def test_catalog_api_exposure_rejects_unknown_modes() -> None:
    with pytest.raises(ValueError, match="api must be a boolean"):
        catalog_to_dict(
            {"operations": [{"id": "unknown", "method": "GET", "api": "futureMode"}]}
        )


def test_browser_session_api_exposure_sets_private_legacy_wire_value() -> None:
    proto = catalog_to_proto(
        Catalog(
            operations=[
                CatalogOperation(id="browser", method="GET", api="browserSession")
            ]
        )
    )
    assert proto is not None
    operation = proto.operations[0]
    assert operation.api is False
    assert operation.api_mode == 1
    assert operation.HasField("api")
    assert operation.HasField("api_mode")
    assert catalog_to_dict(proto, field_style="json")["operations"] == [
        {"id": "browser", "method": "GET", "api": "browserSession"}
    ]
