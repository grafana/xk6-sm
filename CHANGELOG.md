# Changelog

## [0.5.4](https://github.com/grafana/xk6-sm/compare/v0.5.3...v0.5.4) (2025-04-30)


### Fixes

* Resolve issues reported by zizmor ([#120](https://github.com/grafana/xk6-sm/issues/120)) ([2b0fa18](https://github.com/grafana/xk6-sm/commit/2b0fa18e5bd85fb4bf9608bf661d34d53a17e768))

## [0.5.3](https://github.com/grafana/xk6-sm/compare/v0.5.2...v0.5.3) (2025-04-22)


### Fixes

* upgrade k6 to include a bug fix ([#117](https://github.com/grafana/xk6-sm/issues/117)) ([4515b23](https://github.com/grafana/xk6-sm/commit/4515b2347b52e26cc14d2a0a60d5666648242d04))


### Miscellaneous Chores

* Update actions/create-github-app-token action to v2 ([#111](https://github.com/grafana/xk6-sm/issues/111)) ([6b8702c](https://github.com/grafana/xk6-sm/commit/6b8702c4c0ba72d2be2ce43efdeddabee03f32c9))
* Update actions/setup-go digest to 0aaccfd ([#96](https://github.com/grafana/xk6-sm/issues/96)) ([379464f](https://github.com/grafana/xk6-sm/commit/379464fea663ce0b8b62f2a2b27365ca1ffe49ce))
* Update golangci/golangci-lint-action action to v7 ([#105](https://github.com/grafana/xk6-sm/issues/105)) ([147bc11](https://github.com/grafana/xk6-sm/commit/147bc1149e262f5b66f34f71ec7c2a32383f6f24))
* Update gsm-api-go-client digest to 13991b8 ([#113](https://github.com/grafana/xk6-sm/issues/113)) ([ba0c8f0](https://github.com/grafana/xk6-sm/commit/ba0c8f061a90de37f5d7d0a72ffaa58c8fc5407c))

## [0.5.2](https://github.com/grafana/xk6-sm/compare/v0.5.1...v0.5.2) (2025-04-04)


### Miscellaneous Chores

* Update dependency go to v1.24.2 ([58d21f5](https://github.com/grafana/xk6-sm/commit/58d21f52f75f0c633b68d858bdde0e246263e08e))
* Update gsm-api-go-client digest to 0505783 ([#110](https://github.com/grafana/xk6-sm/issues/110)) ([681a7b5](https://github.com/grafana/xk6-sm/commit/681a7b5992db51fe3aad2917067c0b42a548691f))
* Update gsm-api-go-client digest to e75dbea ([#107](https://github.com/grafana/xk6-sm/issues/107)) ([f01a772](https://github.com/grafana/xk6-sm/commit/f01a772ec798564c56b4753726e699a7d0db3735))
* Update module github.com/spf13/afero to v1.14.0 ([#94](https://github.com/grafana/xk6-sm/issues/94)) ([4c8412e](https://github.com/grafana/xk6-sm/commit/4c8412e4503493e93d7db718a811141b5c4856e7))
* Upgrade k6 to v0.58.0 and remove gsm binary ([#106](https://github.com/grafana/xk6-sm/issues/106)) ([c87c83a](https://github.com/grafana/xk6-sm/commit/c87c83ab559d31f04bd368b804c81df41fec4eaa))

## [0.5.1](https://github.com/grafana/xk6-sm/compare/v0.5.0...v0.5.1) (2025-03-28)


### Miscellaneous Chores

* Update gsm-api-go-client digest to 79d3e7c ([#103](https://github.com/grafana/xk6-sm/issues/103)) ([d6684f2](https://github.com/grafana/xk6-sm/commit/d6684f212e0306f5fa1eca61ef6299787c593da8))

## [0.5.0](https://github.com/grafana/xk6-sm/compare/v0.4.1...v0.5.0) (2025-03-25)


### Features

* do not output timeseries whose `resource_type` does not match an allowlist ([a192663](https://github.com/grafana/xk6-sm/commit/a1926630296d975a98b3492949f073528f01be11))


### Fixes

* Add a Makefile ([#100](https://github.com/grafana/xk6-sm/issues/100)) ([ed543b4](https://github.com/grafana/xk6-sm/commit/ed543b41ab010b8b0693b5f2d1f2a818ddea3d32))


### Miscellaneous Chores

* integration: add tests for browser metric source allowlisting ([01ce7ba](https://github.com/grafana/xk6-sm/commit/01ce7ba96b2638631108206f95b110d5369ee17a))
* Update ghcr.io/grafana/crocochrome Docker tag to v0.5.2 ([#99](https://github.com/grafana/xk6-sm/issues/99)) ([a92f83c](https://github.com/grafana/xk6-sm/commit/a92f83c4179ab4b6d3c118b6852f80d9554a5e2e))

## [0.4.1](https://github.com/grafana/xk6-sm/compare/v0.4.0...v0.4.1) (2025-03-20)


### Miscellaneous Chores

* Automatically upgrade the gsm-api-go-client ([#88](https://github.com/grafana/xk6-sm/issues/88)) ([60a7457](https://github.com/grafana/xk6-sm/commit/60a74573284c8f29baf9fbdb5f39c1f165557f4d))
* enable renovate dry run ([#91](https://github.com/grafana/xk6-sm/issues/91)) ([7ccadc4](https://github.com/grafana/xk6-sm/commit/7ccadc440c351ad719845bbf1cae45cf5a8ed5e0))
* enable renvoate on pull requests ([#92](https://github.com/grafana/xk6-sm/issues/92)) ([e41a51a](https://github.com/grafana/xk6-sm/commit/e41a51a4d1ad62ec6e5bbf1479dd0b67a9b338dc))
* Update ghcr.io/grafana/crocochrome Docker tag to v0.5.1 ([#87](https://github.com/grafana/xk6-sm/issues/87)) ([c09796c](https://github.com/grafana/xk6-sm/commit/c09796ce182370913bcf8f960c0a51942e4f0241))
* Update golangci/golangci-lint-action digest to 4696ba8 ([#86](https://github.com/grafana/xk6-sm/issues/86)) ([4e90459](https://github.com/grafana/xk6-sm/commit/4e904599466f26060aa47e29efd8b75def54ac1d))
* Update gsm-api-go-client digest to bd5bcca ([#93](https://github.com/grafana/xk6-sm/issues/93)) ([5c7bfaf](https://github.com/grafana/xk6-sm/commit/5c7bfaf4fd38c439dcbe7416e715364eaf89731f))
* Update module github.com/prometheus/common to v0.63.0 ([#89](https://github.com/grafana/xk6-sm/issues/89)) ([7bede59](https://github.com/grafana/xk6-sm/commit/7bede59c720eac9deae883a2f342addb8cf34f33))

## [0.4.0](https://github.com/grafana/xk6-sm/compare/v0.3.0...v0.4.0) (2025-03-11)


### Features

* build a second binary with the Grafana secrets manager client extension ([#75](https://github.com/grafana/xk6-sm/issues/75)) ([31f4734](https://github.com/grafana/xk6-sm/commit/31f4734d1f4b435eed29b811ccc80d66e0a814c5))
* update k6 to 0.57.0 ([0389dce](https://github.com/grafana/xk6-sm/commit/0389dcea4ca707f7b3df46ec193b9e65e9dc7a13))


### Fixes

* correctly handle __raw_url__ by replacing url with it if present ([1b9b29d](https://github.com/grafana/xk6-sm/commit/1b9b29d868c5dcda37a25a58aa655c54dcf77122))
* handle abbreviated `proto` tags such as `h2` or `h3` ([7a3393e](https://github.com/grafana/xk6-sm/commit/7a3393e00e1e42813a6bc8237e43b4c639fdcba4))


### Miscellaneous Chores

* integration: add tests for __raw_url__ handling ([67c8086](https://github.com/grafana/xk6-sm/commit/67c80866e38f82902731e8028ff8f93551790e43))
* integration: add tests for browser scripts and metrics ([3a9bb2a](https://github.com/grafana/xk6-sm/commit/3a9bb2a4b64b228c6b64df77c03f864ca0f87dd2))
* integration: extract script run to a helper function ([965928d](https://github.com/grafana/xk6-sm/commit/965928d23c60ac5d3a8a4229b79d85887a21e706))
* integration: increase k6 timeout ([8873a57](https://github.com/grafana/xk6-sm/commit/8873a5743a77c40fdf7ca1972e3cac3fab092be2))
* integration: log k6 output if it fails to run ([96ff5a5](https://github.com/grafana/xk6-sm/commit/96ff5a59de30d5dbb8d62a01ca6882c3a3aee2aa))
* README: clarify this repo is not to be used by SM end users ([#71](https://github.com/grafana/xk6-sm/issues/71)) ([e456446](https://github.com/grafana/xk6-sm/commit/e4564463db1cdb70fe36b55a2600ed19c59d361b))
* remove unused renovate-app.json ([296c26c](https://github.com/grafana/xk6-sm/commit/296c26c7800b6d739379e433b21cfd3e8f778fd5))
* renovate: update crocochrome image used for testing ([7d2f8b6](https://github.com/grafana/xk6-sm/commit/7d2f8b6b25984b97868c9cc1e185edd80590fb23))
* Update actions/create-github-app-token digest to 21cfef2 ([cc5c756](https://github.com/grafana/xk6-sm/commit/cc5c756172b7b59cfa5b505af989a0ba0ff295e7))
* Update dependency go to v1.24.1 ([#76](https://github.com/grafana/xk6-sm/issues/76)) ([9e7b76c](https://github.com/grafana/xk6-sm/commit/9e7b76c8a418fb6f0b35e2ba55e89530ee504d7f))
* Update googleapis/release-please-action digest to a02a34c ([#83](https://github.com/grafana/xk6-sm/issues/83)) ([e670179](https://github.com/grafana/xk6-sm/commit/e670179f031e908b1d0d61a29d2c8bcb1a4b2fe2))
* use non-deprecated prometheus format selection ([7c3de2a](https://github.com/grafana/xk6-sm/commit/7c3de2aa9f32538715daab1b8559287415eb67ab))

## [0.3.0](https://github.com/grafana/xk6-sm/compare/v0.2.0...v0.3.0) (2025-02-26)


### Features

* add policy bot configuration ([#48](https://github.com/grafana/xk6-sm/issues/48)) ([fdc3693](https://github.com/grafana/xk6-sm/commit/fdc36935c77af5cd58fd8e32c32d4d116592ac2c))
* Add release-please ([#49](https://github.com/grafana/xk6-sm/issues/49)) ([cdd5798](https://github.com/grafana/xk6-sm/commit/cdd579897680e3e57b39674548a882e0c1f2048b))
* refactor metrics processing ([#34](https://github.com/grafana/xk6-sm/issues/34)) ([eaa5fc3](https://github.com/grafana/xk6-sm/commit/eaa5fc347afdf4425a805da11eb5fd419cff318c))


### Fixes

* Fix release-please manifest file ([#51](https://github.com/grafana/xk6-sm/issues/51)) ([238d945](https://github.com/grafana/xk6-sm/commit/238d945909aae394c6e45eeaa11311d87c61ef14))


### Miscellaneous Chores

* create `dist` directory so `xk6` can write compiled binaries to it ([dfe362a](https://github.com/grafana/xk6-sm/commit/dfe362ac7e841b4e3188af4e8fe973afffaea2a6))
* integration: add tests for custom phases ([9c682a7](https://github.com/grafana/xk6-sm/commit/9c682a7dc80c7e226d8d5f7752fc3d0bd78c9ed5))
* integration: properly parse output metrics and assert more things about them ([#44](https://github.com/grafana/xk6-sm/issues/44)) ([510f2cc](https://github.com/grafana/xk6-sm/commit/510f2ccf97c82168d55cf45cc4a34eb724b2367e))
* README: adjust release process description ([#59](https://github.com/grafana/xk6-sm/issues/59)) ([aaeaded](https://github.com/grafana/xk6-sm/commit/aaeadedcfa2332b3636efd62ffc5c514781ae2d1))
* renovate: use prefix from preset ([6053d6c](https://github.com/grafana/xk6-sm/commit/6053d6c57a2c7c01b8924cae7a391a7520240ce0))
* Update actions/create-github-app-token digest to 0d56448 ([67fb83d](https://github.com/grafana/xk6-sm/commit/67fb83d7d78bf18d17c78acc4b032ea8036828d8))
* update CODEOWNERS ([23b5d3f](https://github.com/grafana/xk6-sm/commit/23b5d3fb5b314814880b9a8af58302e4c4cb0f64))
* Update dependency go to v1.24.0 ([1acf382](https://github.com/grafana/xk6-sm/commit/1acf382c8400ba6df8342150be1174216def399a))
* Update golangci/golangci-lint-action digest to 0adbc47 ([b4c424b](https://github.com/grafana/xk6-sm/commit/b4c424b8f7140b123b09fe2dfd8473806f4acbee))
* Update golangci/golangci-lint-action digest to 2226d7c ([bd43062](https://github.com/grafana/xk6-sm/commit/bd43062f3d1f440278d041833f0e08ad86265bb6))
* Update golangci/golangci-lint-action digest to 818ec4d ([#64](https://github.com/grafana/xk6-sm/issues/64)) ([40d712c](https://github.com/grafana/xk6-sm/commit/40d712ca779d10c8adb1ca993c1c80c9bbede372))
* Update golangci/golangci-lint-action digest to e0ebdd2 ([65e3320](https://github.com/grafana/xk6-sm/commit/65e33200b43ad26b3551f367a82cfc3ddff627c4))
* Update prometheus-go ([68a435c](https://github.com/grafana/xk6-sm/commit/68a435c638bf6f3c244dde7a7810d0a6562c3234))
* use fully qualified package name ([8beec7f](https://github.com/grafana/xk6-sm/commit/8beec7f0db5e2fc8ed3e4cf2254f69c2d38997ab))
