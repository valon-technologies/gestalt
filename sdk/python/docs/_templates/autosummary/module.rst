{{ fullname | escape | underline }}

.. automodule:: {{ fullname }}
   :members:
   :undoc-members:
   :show-inheritance:
{%- if fullname == "gestalt" %}
   :imported-members:
{# autodoc cannot see module data resolved through the package's lazy
   __getattr__; conf.py computes exactly that remainder. #}
{%- if gestalt_lazy_data %}

.. rubric:: Constants and type aliases
{% for item in gestalt_lazy_data %}
.. autodata:: {{ fullname }}.{{ item }}
   :no-value:
{% endfor %}
{%- endif %}
{%- endif %}

{% block modules %}
{%- set visible = [] %}
{%- for item in modules %}
{%- if not item.split('.')[-1].startswith('_') %}
{%- set _ = visible.append(item) %}
{%- endif %}
{%- endfor %}
{%- if visible %}
.. rubric:: Modules

.. autosummary::
   :toctree:
   :recursive:
{% for item in visible %}
   {{ item }}
{%- endfor %}
{%- endif %}
{% endblock %}
