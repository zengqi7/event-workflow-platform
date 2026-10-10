# Security Policy

## Supported scope

The latest commit on `main` receives security fixes. This V1 service is
designed for a trusted internal network behind an authenticated gateway. The
tenant selector in the API is not an authentication mechanism.

## Reporting

Use GitHub private vulnerability reporting when it is enabled for the
repository. Do not include credentials, production data, or exploit details in
a public issue.

Include the affected endpoint or component, reproduction steps, impact, and a
suggested mitigation when available.

## Deployment guidance

- Keep API, MySQL, Redis, and the demo Webhook sink bound to trusted networks.
- Replace every value copied from `.env.example`.
- Put TLS, authentication, authorization, and request-rate limits at the
  ingress layer before exposing the API outside a development environment.
- Treat Webhook destinations as privileged configuration and restrict who can
  edit workflow definitions.
