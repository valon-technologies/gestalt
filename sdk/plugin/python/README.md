# Gestalt Plugin SDK for Python

This is a thin helper for implementing a Gestalt subprocess plugin in Python.

```py
from gestalt_plugin import Plugin, PluginInfo, ProviderManifest, OperationDef

plugin = Plugin(
    plugin_info=PluginInfo(name="loan-enrichment", version="0.1.0"),
    provider=ProviderManifest(
        display_name="Loan Enrichment",
        description="Fetches enrichment data for a loan",
        connection_mode="user",
        operations=[
            OperationDef(
                name="enrich_loan",
                description="Fetch enrichment data",
                method="POST",
                parameters=[{"name": "loan_id", "type": "string", "required": True}],
            )
        ],
    ),
)

@plugin.execute
def execute(request):
    return {"status": 200, "body": "{\"ok\": true}"}

plugin.serve()
```

The SDK reads framed JSON-RPC messages from stdin and writes responses to
stdout. Use stderr for logs.

