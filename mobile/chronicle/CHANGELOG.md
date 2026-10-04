# Changelog

## [0.3.0](https://github.com/Einlanzerous/chronicle/compare/mobile-v0.2.0...mobile-v0.3.0) (2026-10-04)


### Features

* **api:** notes-only search any account may call — GET /notes/search (CHRN-116) ([#152](https://github.com/Einlanzerous/chronicle/issues/152)) ([5e3f958](https://github.com/Einlanzerous/chronicle/commit/5e3f958a1a3416901882ce4b8f975f9124059410))
* **api:** pin a memo's audio after the fact — raise retention from the note view (CHRN-128) ([#144](https://github.com/Einlanzerous/chronicle/issues/144)) ([7e8b33c](https://github.com/Einlanzerous/chronicle/commit/7e8b33ce10d2ae1744dd28f686c83f7a4d866ec4))
* **mobile:** on-device batch triage with a ticket deep link (CHRN-63) ([#147](https://github.com/Einlanzerous/chronicle/issues/147)) ([703afaf](https://github.com/Einlanzerous/chronicle/commit/703afaf34e0ea297434b4f0752293cd921d90fa9))


### Bug Fixes

* **mobile:** triage files every complete proposal, a held discard says it will discard, a refusal is a sentence (CHRN-137) ([#151](https://github.com/Einlanzerous/chronicle/issues/151)) ([24b939f](https://github.com/Einlanzerous/chronicle/commit/24b939f32a238c035e9f4aeeeea2225bea903cb1))

## 0.2.0 (2026-09-27)


### Features

* **api:** carry the recording time to the server, display-only, never the prune clock (CHRN-118) ([#125](https://github.com/Einlanzerous/chronicle/issues/125)) ([141d7db](https://github.com/Einlanzerous/chronicle/commit/141d7dbb68815cc00dbb7dd265a62101f2422296))
* **mobile:** an Android release track -- signed APKs on mobile-v* tags, with a guard that rejects --dart-define (CHRN-125) ([#134](https://github.com/Einlanzerous/chronicle/issues/134)) ([d91a825](https://github.com/Einlanzerous/chronicle/commit/d91a825eddd89858aa9254c02d1250088261933b))
* **mobile:** choose the audio's retention right after recording, and hold the undecided (CHRN-62) ([#133](https://github.com/Einlanzerous/chronicle/issues/133)) ([661ab3f](https://github.com/Einlanzerous/chronicle/commit/661ab3f1f7a4c5d3d2b91f3df849ef64594ba12c))
* **mobile:** dismiss an empty capture -- hide, never delete (CHRN-119) ([#126](https://github.com/Einlanzerous/chronicle/issues/126)) ([8e846f4](https://github.com/Einlanzerous/chronicle/commit/8e846f4ff4e8b6a19611ba13ce38c3f286322edd))
* **mobile:** durable offline queue that survives a force-stop (CHRN-61) ([#123](https://github.com/Einlanzerous/chronicle/issues/123)) ([e16d9ec](https://github.com/Einlanzerous/chronicle/commit/e16d9ec0ba07be446215806a7113f3f8de3c1dc5))
* **mobile:** generate the Android client's API package from openapi.yaml (CHRN-59) ([#115](https://github.com/Einlanzerous/chronicle/issues/115)) ([3fc94d7](https://github.com/Einlanzerous/chronicle/commit/3fc94d77f19892cddc8110cbb7d43b7902e0ef27))
* **mobile:** one-tap capture — Ogg/Opus into a foreground service, and a salvage that destroys nothing (CHRN-60) ([#119](https://github.com/Einlanzerous/chronicle/issues/119)) ([4dc6a84](https://github.com/Einlanzerous/chronicle/commit/4dc6a8401a439165777bc0b0c3a06c7db6b6c76b))
* **mobile:** prune local audio only on the server's own prune, default-off (CHRN-120) ([#128](https://github.com/Einlanzerous/chronicle/issues/128)) ([27ef45f](https://github.com/Einlanzerous/chronicle/commit/27ef45f74fe860b04b777f67086d77bd48a35bcc))
* **mobile:** the Android app — auth against Chronicle's own tokens, and networking that says what is wrong (CHRN-59) ([#116](https://github.com/Einlanzerous/chronicle/issues/116)) ([d46a2f7](https://github.com/Einlanzerous/chronicle/commit/d46a2f72f1d88e543eaaef967534f4d65fc8a1e7))
* **web:** show when a memo was recorded, not when it arrived (CHRN-123) ([#130](https://github.com/Einlanzerous/chronicle/issues/130)) ([7aff29d](https://github.com/Einlanzerous/chronicle/commit/7aff29d1e6c5b3fc932fcadb08f751d775c91bfa))


### Bug Fixes

* **api:** state one duration for a memo on every surface that shows one (CHRN-85) ([#132](https://github.com/Einlanzerous/chronicle/issues/132)) ([1b3d1ec](https://github.com/Einlanzerous/chronicle/commit/1b3d1ecd0f2a88d725368001a86d621c3885c68c))
* **mobile:** a recovered whole capture is ready, marked recovered, and stored once (CHRN-114) ([#121](https://github.com/Einlanzerous/chronicle/issues/121)) ([ca14aa9](https://github.com/Einlanzerous/chronicle/commit/ca14aa931fa3526b9ddcb2290d8d0b6053263492))
