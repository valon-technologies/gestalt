Gestalt Python SDK
==================

Use :mod:`gestalt` to build executable Gestalt providers with Python models,
decorators, runtime providers, host-service clients, and telemetry helpers.

The package is published as ``gestalt-sdk`` and imported as ``gestalt`` in
provider projects.

This reference focuses on the handwritten Python SDK surface. Runtime transport
internals are intentionally excluded from these authored pages.

API sections
------------

The API reference is organized by the provider-authoring workflow:

* :ref:`Core authoring types <python-core-authoring-types>` for models,
  request and response wrappers, and operation results.
* :ref:`App authoring <python-app-authoring>` for executable app
  definitions, catalogs, session catalogs, and HTTP subject resolution.
* :ref:`Workflow helpers <python-workflow-helpers>` for native workflow target,
  run, schedule, signal, and event values.
* :ref:`Agent provider models <python-agent-provider-models>` for agent
  sessions, turns, messages, tools, and provider responses.
* :ref:`Provider interfaces <python-provider-interfaces>` for executable
  provider surfaces and authentication payloads.
* :ref:`Provider telemetry <python-provider-telemetry>` for provider-authored
  GenAI spans and metrics.
* :ref:`Storage and host-service clients <python-storage-and-host-service-clients>` for
  the IndexedDB driver, S3 provider helpers, and runtime-log clients, plus the
  generated provider clients for cache, S3, app invocation, and agent
  management.

.. toctree::
   :maxdepth: 2
   :caption: Reference

   reference
