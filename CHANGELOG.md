# Changelog

## [1.1.0](https://github.com/Einlanzerous/chronicle/compare/v1.0.0...v1.1.0) (2026-08-27)


### Features

* **api:** resumable memo upload for the app queue (CHRN-20) ([469da40](https://github.com/Einlanzerous/chronicle/commit/469da40e1d38b0d5fa93c95496a9939fff134a55))
* **api:** resumable memo upload for the app queue (CHRN-20) ([7cecd47](https://github.com/Einlanzerous/chronicle/commit/7cecd47a617ce2bf2ec5409a8b62d26e98a3df03))
* **audio:** audio metadata from the Opus headers (CHRN-21) ([77844d8](https://github.com/Einlanzerous/chronicle/commit/77844d894f3b2c6e5810d1a17e851722ef5a2cd9))
* **audio:** read duration, codec and sample rate from the Opus headers (CHRN-21) ([50b4b4e](https://github.com/Einlanzerous/chronicle/commit/50b4b4e021118c4376bf1eca3c4a1cab78152a99))
* **audio:** storage layout and disk budget accounting (CHRN-23) ([bff9353](https://github.com/Einlanzerous/chronicle/commit/bff9353faaf34a4ca5688453722d5127d165eefc))
* **audio:** storage layout and disk budget accounting (CHRN-23) ([6fc6211](https://github.com/Einlanzerous/chronicle/commit/6fc6211a5161d4f5e60a79abb3007d156b20d50a))
* **store:** record what a recording is, on both ingest paths (CHRN-21) ([17f201a](https://github.com/Einlanzerous/chronicle/commit/17f201af6650533d7682a59efb63e22318a9a41a))
* **store:** the memo model and the idempotency rule (CHRN-18) ([ea1b4b0](https://github.com/Einlanzerous/chronicle/commit/ea1b4b0c5a663bbba4984921aa1603977be6d405))
* **store:** the memo model and the idempotency rule (CHRN-18) ([78b3bfb](https://github.com/Einlanzerous/chronicle/commit/78b3bfbb976d8739c1dd4e014d0a5a0454cedcde))
* **store:** upload sessions, in tier 1 (CHRN-20) ([bb64a2b](https://github.com/Einlanzerous/chronicle/commit/bb64a2bf58821cd8ab01fac6e9ad17ae091be5be))
* **watch:** the Copyparty capture seam (CHRN-19) ([2178c21](https://github.com/Einlanzerous/chronicle/commit/2178c21688a32046a52b252669cde93c67a0285e))
* **watch:** the Copyparty capture seam (CHRN-19) ([0664c55](https://github.com/Einlanzerous/chronicle/commit/0664c5574d1603c5104c850a79a42d61aa7d9277))


### Bug Fixes

* **api:** trust a signal only Traefik can produce (CHRN-75) ([fcf22f0](https://github.com/Einlanzerous/chronicle/commit/fcf22f0401413b1cd25c7e4d2b08ce860899334f))
* **api:** trust a signal only Traefik can produce (CHRN-75) ([e6976d6](https://github.com/Einlanzerous/chronicle/commit/e6976d68b98aa2cbbf4a39f660d6a83a83da5ee0))
* **audio:** the five nits from the reviewer's read of PR [#11](https://github.com/Einlanzerous/chronicle/issues/11) (CHRN-23) ([d1f6abf](https://github.com/Einlanzerous/chronicle/commit/d1f6abf5e7d83947c9aac6664c36b6be9b807297))
* **audio:** validate a page header before reading a granule (CHRN-21) ([f032177](https://github.com/Einlanzerous/chronicle/commit/f03217710c3d0cb8aeeee0865df11a9354f4c092))
* **scripts:** stop gen-schema.sh from aiming a DROP at the shared Postgres (CHRN-77) ([64657eb](https://github.com/Einlanzerous/chronicle/commit/64657eb4a6f2856dbcb02fad95db9880a613fff7))
* **scripts:** stop gen-schema.sh from aiming a DROP at the shared Postgres (CHRN-77) ([6d43763](https://github.com/Einlanzerous/chronicle/commit/6d4376310e813351a9726f5615f643aaf865dfe7))
* **store:** report the collapse instead of inferring it (CHRN-18) ([95815c4](https://github.com/Einlanzerous/chronicle/commit/95815c4f19eb2b08da07a9dc4fee5fe0537077f1))
* **upload:** do not commit a memo whose audio is not on disk (CHRN-20) ([6b3ab00](https://github.com/Einlanzerous/chronicle/commit/6b3ab000c0a14a868e9c7869c16f735ead007013))
* **watch:** the four nits from the reviewer's read of PR [#12](https://github.com/Einlanzerous/chronicle/issues/12) (CHRN-19) ([5316a47](https://github.com/Einlanzerous/chronicle/commit/5316a47844ff0f66f7f6c737554f92b069b41ff4))

## 1.0.0 (2026-08-25)


### Features

* **auth:** accounts, invites and per-device sessions ([69c7164](https://github.com/Einlanzerous/chronicle/commit/69c7164a8ef3e5e66c4f9b9fd3c6eeed6db2fd08))
* **auth:** accounts, invites and per-device sessions (CHRN-71) ([d1e3010](https://github.com/Einlanzerous/chronicle/commit/d1e3010c2773bda1ca8d7e2896578175564f755b))
* **db:** chronicle database, roles, and the up/down migration harness ([c0a7ef5](https://github.com/Einlanzerous/chronicle/commit/c0a7ef56bc2025a96c37cbbf0d5fe362b83b511f))
* **deploy:** container image, compose service, and Traefik routers ([47bfd56](https://github.com/Einlanzerous/chronicle/commit/47bfd56cdf962bdcc04765fc1785ae58a4190f72))
* repo, CLAUDE.md, and the two-tier doctrine README ([504b73d](https://github.com/Einlanzerous/chronicle/commit/504b73d0cb135dd84bbbe88ed1a0978c103d8465))
* **service:** config, health probes, structured logs, graceful shutdown ([3930641](https://github.com/Einlanzerous/chronicle/commit/3930641cd735c1adbd7b17d9e398994af6bf1da6))


### Bug Fixes

* **auth:** close the guards that did not hold under the real deployment ([c49918a](https://github.com/Einlanzerous/chronicle/commit/c49918a8b6f3e404e59ce94ec9a36820917af3d8))
* **ci:** one sha tag per build, and serialise pushes to :latest (CHRN-73) ([317dcb9](https://github.com/Einlanzerous/chronicle/commit/317dcb9282115731e3240e4aa065c6ffe86d58c4))
* **deploy:** close the three findings from the reviewer's read (CHRN-16) ([a86c1bb](https://github.com/Einlanzerous/chronicle/commit/a86c1bb7d4ec46286d4510c57038cedaae43fb31))
* **deploy:** correct the Traefik fragment against the live edge (CHRN-16) ([7544a0b](https://github.com/Einlanzerous/chronicle/commit/7544a0b2eb215619126b385d73fc9c7662ddf3c7))
* **deploy:** correct the Traefik fragment against the live edge (CHRN-16) ([fb2e79a](https://github.com/Einlanzerous/chronicle/commit/fb2e79ac98733bea43eb5cd03584d20ee5fc580f))
