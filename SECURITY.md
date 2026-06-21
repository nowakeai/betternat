# Security Policy

## Supported Versions

BetterNAT is pre-release software. Until `v1.0.0`, security fixes are provided for the latest released version only.

## Reporting A Vulnerability

Do not open a public GitHub issue for a suspected vulnerability.

Report security issues privately to the maintainers through GitHub private vulnerability reporting when available, or by contacting the repository owner directly.

Include:

- affected version or commit,
- deployment mode,
- cloud/region if relevant,
- impact,
- reproduction steps,
- logs or metrics with secrets removed,
- whether the issue is already exploited or publicly known.

## Scope

Security-sensitive areas include:

- AWS IAM policy and role assumptions,
- EC2 route table mutation,
- EIP association,
- DynamoDB lease/fencing,
- agent config handling,
- metrics exposure,
- AMI/bootstrap supply chain,
- local datapath rule generation.

## Disclosure

The maintainers will acknowledge valid reports when practical, investigate, prepare fixes, and publish release notes with appropriate detail.

Please give maintainers reasonable time to fix confirmed vulnerabilities before public disclosure.
