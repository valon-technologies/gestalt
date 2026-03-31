# Docker deployment examples

This directory contains published-image examples for `valontechnologies/gestaltd`.

- `config.yaml`: minimal static config that works with the default image command
- `Dockerfile`: multi-stage example that inits config during the image build
- `Dockerfile.plugins`: multi-stage example that compiles Go plugins with a standard `golang:` image, packages them, and initializes lock state for deployment
