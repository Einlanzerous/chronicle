# Changelog

## [1.8.0](https://github.com/Einlanzerous/chronicle/compare/v1.7.0...v1.8.0) (2026-09-05)


### Features

* **markdown:** store raw, render on read, and leave references alone (CHRN-40) ([#57](https://github.com/Einlanzerous/chronicle/issues/57)) ([ed22837](https://github.com/Einlanzerous/chronicle/commit/ed228370d5b74cb60889c7fefeaffad79239f286))
* **notes:** backlinks derived, tags authored, and the tier line between them (CHRN-42) ([#59](https://github.com/Einlanzerous/chronicle/issues/59)) ([c07d1dd](https://github.com/Einlanzerous/chronicle/commit/c07d1dddb0583aca6e4c150a0cd46b3f8ab86fd8))
* **notes:** the note, its permanent number, and text that lives in revisions (CHRN-38) ([#56](https://github.com/Einlanzerous/chronicle/issues/56)) ([68d3697](https://github.com/Einlanzerous/chronicle/commit/68d369766dd87c4c3142abcd79d8a39686f47077))
* **pages:** the page tree, and a move that orphans nothing (CHRN-37) ([#55](https://github.com/Einlanzerous/chronicle/issues/55)) ([2d1b26c](https://github.com/Einlanzerous/chronicle/commit/2d1b26c82ff04c4bf04399fac855020b84cbeb0d))
* **search:** Postgres FTS over notes and transcripts, with no second copy (CHRN-41) ([#58](https://github.com/Einlanzerous/chronicle/issues/58)) ([9e2e3b5](https://github.com/Einlanzerous/chronicle/commit/9e2e3b55e838419fde3719e63975f7b557ab1d0d))

## [1.7.0](https://github.com/Einlanzerous/chronicle/compare/v1.6.0...v1.7.0) (2026-09-05)


### Features

* **triage:** HOLD parks a decision, DISCARD marks without destroying (CHRN-34) ([#53](https://github.com/Einlanzerous/chronicle/issues/53)) ([935317c](https://github.com/Einlanzerous/chronicle/commit/935317c4db9d9a8ded9d421f73723516fbef66c7))

## [1.6.0](https://github.com/Einlanzerous/chronicle/compare/v1.5.0...v1.6.0) (2026-09-05)


### Features

* **catalogue:** the live Switchyard project list (CHRN-31) ([#47](https://github.com/Einlanzerous/chronicle/issues/47)) ([569f838](https://github.com/Einlanzerous/chronicle/commit/569f8387f5a1afeae0f5e248397bfe83b899e2a9))
* **eval:** the routing eval set and its harness (CHRN-36) ([#44](https://github.com/Einlanzerous/chronicle/issues/44)) ([a3ea527](https://github.com/Einlanzerous/chronicle/commit/a3ea527f4be272341be987ce04502e086f17df96))
* **scribe:** routing prompt v1 on local Gemma 4, and the catalogue it renders from (CHRN-30) ([#46](https://github.com/Einlanzerous/chronicle/issues/46)) ([54998fa](https://github.com/Einlanzerous/chronicle/commit/54998fafbfe20bfc3e68d301df7e7122d479f5dd))
* **scribe:** set the pre-accept threshold from the scored eval (CHRN-36) ([#52](https://github.com/Einlanzerous/chronicle/issues/52)) ([b0efbb0](https://github.com/Einlanzerous/chronicle/commit/b0efbb0599d5316ef113aaa9ac7d77f1234b4b56))
* **triage:** propose N, accept many, survive a partial failure (CHRN-33) ([#50](https://github.com/Einlanzerous/chronicle/issues/50)) ([253fadb](https://github.com/Einlanzerous/chronicle/commit/253fadb3b50a838edb224ddd0fbc490fd96239df))

## [1.5.0](https://github.com/Einlanzerous/chronicle/compare/v1.4.0...v1.5.0) (2026-08-30)


### Features

* **scribe:** the proposal contract, and the grant that lets tier 1 read its own corpus (CHRN-32) ([#42](https://github.com/Einlanzerous/chronicle/issues/42)) ([f6f30a9](https://github.com/Einlanzerous/chronicle/commit/f6f30a910823dd107f7331fe4ba673fe077b9316))


### Bug Fixes

* **asr:** decode wav by not overwriting its own input (CHRN-84) ([#39](https://github.com/Einlanzerous/chronicle/issues/39)) ([2a1cb88](https://github.com/Einlanzerous/chronicle/commit/2a1cb887e28a1b7233e7d138bf5a02615e4fc28b))
* **publish:** one writer for :latest, and gate the sha tag on a validated version (SERV-154) ([#37](https://github.com/Einlanzerous/chronicle/issues/37)) ([4e4f732](https://github.com/Einlanzerous/chronicle/commit/4e4f7328029d13a1ed551c5a6eb1efe614c64d7e))


### Maintenance

* **main:** release asr 0.1.1 ([#41](https://github.com/Einlanzerous/chronicle/issues/41)) ([086acf8](https://github.com/Einlanzerous/chronicle/commit/086acf8d2540d85102ae1de686561c502f8841c2))

## [1.4.0](https://github.com/Einlanzerous/chronicle/compare/v1.3.0...v1.4.0) (2026-08-29)


### Features

* **asr:** publish estate-asr on main and asr-v* tags (CHRN-82) ([#34](https://github.com/Einlanzerous/chronicle/issues/34)) ([4699a44](https://github.com/Einlanzerous/chronicle/commit/4699a44a67bc1e1c4137e7d12500b435ca70da29))


### Code Refactoring

* **asr:** move the service under asr/ and seal the boundary (CHRN-82) ([#32](https://github.com/Einlanzerous/chronicle/issues/32)) ([371c150](https://github.com/Einlanzerous/chronicle/commit/371c150ca46f69f41aa33f5f76eccdbc450f3d3a))


### Maintenance

* **main:** release asr 0.1.0 ([#35](https://github.com/Einlanzerous/chronicle/issues/35)) ([5e56cb9](https://github.com/Einlanzerous/chronicle/commit/5e56cb9c64d9e12f706a86f6dffbe708c64e82e4))
* **release:** the asr package starts at 0.1.0, not 1.0.0 (CHRN-82) ([#36](https://github.com/Einlanzerous/chronicle/issues/36)) ([f746522](https://github.com/Einlanzerous/chronicle/commit/f7465224b40c214ec982d5b37589b7d6924bf445))

## [1.3.0](https://github.com/Einlanzerous/chronicle/compare/v1.2.0...v1.3.0) (2026-08-29)


### Features

* **asr:** the retry ceiling, and a failing memo that retries first (CHRN-28) ([#27](https://github.com/Einlanzerous/chronicle/issues/27)) ([352183b](https://github.com/Einlanzerous/chronicle/commit/352183bb259934030a8dd8bffd319b6ce4c7c4fb))
* **retention:** the pruner, and the model floor that gates it (CHRN-22) ([#29](https://github.com/Einlanzerous/chronicle/issues/29)) ([6cc805f](https://github.com/Einlanzerous/chronicle/commit/6cc805f6ae5af9e144ed7a5dfb14d1781a1bad8b))

## [1.2.0](https://github.com/Einlanzerous/chronicle/compare/v1.1.0...v1.2.0) (2026-08-29)


### Features

* **asr:** pinned whisper.cpp Vulkan image for the R9700 (CHRN-24) ([138af36](https://github.com/Einlanzerous/chronicle/commit/138af36348a6b96f93d200be23067e58a6a38fbb))
* **asr:** pinned whisper.cpp Vulkan image for the R9700 (CHRN-24) ([a4df9bc](https://github.com/Einlanzerous/chronicle/commit/a4df9bcb3de1625e6e3935ae1cf43f5233d612f0))
* **asr:** the index the round-robin claim reads (CHRN-26) ([c6e6010](https://github.com/Einlanzerous/chronicle/commit/c6e6010f76338b9c39b9364aaef66f85ce521e69))
* **asr:** the job table, the lease, and the reaper (CHRN-25) ([44b77b2](https://github.com/Einlanzerous/chronicle/commit/44b77b20375242b71d4e18c6212c217768de238d))
* **asr:** the resident worker and the single-flight GPU lease (CHRN-26) ([3a15894](https://github.com/Einlanzerous/chronicle/commit/3a15894b6669c6bcc16d23be25b90a47e92548ba))
* **asr:** the resident worker, the device lock and the GPU semaphore (CHRN-26) ([99af3b3](https://github.com/Einlanzerous/chronicle/commit/99af3b3c7c9e1273ee88b529139adac3fa0d719d))
* **asr:** the transcription job contract and its service (CHRN-25) ([59282e4](https://github.com/Einlanzerous/chronicle/commit/59282e4d1047aadfd7d938c1c9678a8f11070349))
* **asr:** the transcription job contract, generated and guarded (CHRN-25) ([0ce22cc](https://github.com/Einlanzerous/chronicle/commit/0ce22cc2f13e53f645b1aaa17c14810831cc1885))
* **transcribe:** a memo becomes a transcript, asynchronously (CHRN-27) ([6f25d48](https://github.com/Einlanzerous/chronicle/commit/6f25d48452038d176a6931fe612912eed977c381))
* **transcribe:** a memo becomes a transcript, asynchronously (CHRN-27) — replayed onto main ([e371182](https://github.com/Einlanzerous/chronicle/commit/e37118296192954928b0e6b0b8ad137adfe879ed))


### Bug Fixes

* **asr:** an absent model is rechecked, not remembered as unloadable (CHRN-26) ([cc5c3a5](https://github.com/Einlanzerous/chronicle/commit/cc5c3a520dcc92e8472a0b98e52ed05255afadc9))
* **asr:** the four nits from the reviewer's read of PR [#17](https://github.com/Einlanzerous/chronicle/issues/17) (CHRN-24) ([8a61132](https://github.com/Einlanzerous/chronicle/commit/8a61132b97b2348fe5f9fe6b51551e17f6ebbfe9))
* **asr:** three findings from the review of cc5c3a5 (CHRN-26) ([138c5c9](https://github.com/Einlanzerous/chronicle/commit/138c5c980d1d7ac6298dc827ac626fb180f1bc12))
* **asr:** three test defects the first CI run and five reruns found (CHRN-26) ([ff708ff](https://github.com/Einlanzerous/chronicle/commit/ff708ff20d9633b153d421b68c5b75544b26d2a3))
* **ci:** stop writing credential-shaped strings the scanner has to judge (CHRN-25) ([26b26af](https://github.com/Einlanzerous/chronicle/commit/26b26af03b3789d89087bf685ed399c7c763bf26))
* **transcribe:** four nits, two of which were comments promising more than the code did (CHRN-27) ([06584fb](https://github.com/Einlanzerous/chronicle/commit/06584fbfe0ffa130acbaa8f64cb796795f43a84e))
* **transcribe:** make retranscribe actually retry, and three nits (CHRN-27) ([2d06ac7](https://github.com/Einlanzerous/chronicle/commit/2d06ac727c0765846cc4b88b36243826897a1cf4))

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
