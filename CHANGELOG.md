# Changelog

All notable changes to this project will be documented in this file.

## [0.8.0](https://github.com/robinvdvleuten/beancount/compare/v0.7.0...v0.8.0) (2026-01-13)


### Features

* add cosign signing and SBOM attestation ([bc56f30](https://github.com/robinvdvleuten/beancount/commit/bc56f309fb6343fba1030ff0d4a2049af81d1d88))
* **cli:** add --host flag to web command ([457a0d7](https://github.com/robinvdvleuten/beancount/commit/457a0d71d8cd3205ec095d38e0f5a397f5b5b9c3))
* generate SBOM for included assets ([96d4f91](https://github.com/robinvdvleuten/beancount/commit/96d4f9180aef907e8286bbfa0f1bc6f67095527f))
* **ledger:** add generic GetBalanceTree API for financial reports ([#117](https://github.com/robinvdvleuten/beancount/issues/117)) ([fe1ce75](https://github.com/robinvdvleuten/beancount/commit/fe1ce75bdc9ed6b87ab8f35e16d82541157e9ca9))
* **ledger:** support custom account names ([67a54ed](https://github.com/robinvdvleuten/beancount/commit/67a54eda7cb7a7347eea42407673b3b3bf250b47))
* release docker image alongside executables ([5ee77dd](https://github.com/robinvdvleuten/beancount/commit/5ee77ddceebd0233dade3a0e192c8431310e9b01))
* serve index.html for all unmatched paths ([551362f](https://github.com/robinvdvleuten/beancount/commit/551362f63e8920c986fe2f92af1c3c48b9f7d27e))
* sign docker manifests upon releases ([4a106b9](https://github.com/robinvdvleuten/beancount/commit/4a106b96110841cb6171887439e342241d05f8d0))
* **web:** add file selector dropdown for included files ([4c5150c](https://github.com/robinvdvleuten/beancount/commit/4c5150c8944b5d14228e17d8440089cfabd3aa52))
* **web:** add sidebar navigation ([2c2b50a](https://github.com/robinvdvleuten/beancount/commit/2c2b50a69c51bba697601fa8929e03cb9bc8f9c3))
* **web:** set up solidjs router ([fc8a20f](https://github.com/robinvdvleuten/beancount/commit/fc8a20fb6e026567760c328bab452d54f69020eb))


### Bug Fixes

* **ledger:** make implicit parent accounts accessible via GetParent/GetChildren ([f6291b9](https://github.com/robinvdvleuten/beancount/commit/f6291b9907e810d1b71e52f4ed59aa64955a31d7))
* **parser:** validate dates at lex time to match beancount behavior ([5e0b8c1](https://github.com/robinvdvleuten/beancount/commit/5e0b8c16c5431b454fa9fcc6823fcd2462590395))
* **web:** filter errors to only show current file ([639ac05](https://github.com/robinvdvleuten/beancount/commit/639ac057faa6f681fbcdc65d76f4f291d6aa10d5))

## [0.7.0](https://github.com/robinvdvleuten/beancount/compare/v0.6.0...v0.7.0) (2026-01-07)


### Features

* add doctor lex command for token debugging ([163ac5b](https://github.com/robinvdvleuten/beancount/commit/163ac5b5c8939088e75b6d28498af2dea03b010e))
* add Must* variants and refactor tests ([d8a788d](https://github.com/robinvdvleuten/beancount/commit/d8a788d28c5db105cf2ff3c8be713fe81b4d736d))
* **ast:** add DirectiveKind with Kind() method, remove Directive() ([bf1442c](https://github.com/robinvdvleuten/beancount/commit/bf1442ca1e17a030433fc5478fee6fab1f59b2c1))
* **ledger:** add ConvertBalance and GetBalanceInCurrency APIs ([56e874d](https://github.com/robinvdvleuten/beancount/commit/56e874d4b443e5890a4aed95596aba6614702ae8))
* **ledger:** add GetAccountsByType() for filtering by account type ([be8c27c](https://github.com/robinvdvleuten/beancount/commit/be8c27cbf30649f5358b4d87c641d6934d8eb508))
* **ledger:** add GetBalanceInCurrencyAsOf plus rename GetBalancesAsOfInCurrency ([ef4db35](https://github.com/robinvdvleuten/beancount/commit/ef4db35900d59016f172ad2172d723e90a16e359))
* **ledger:** add GetBalancesAsOfInCurrency and consolidate account iteration ([ca980a5](https://github.com/robinvdvleuten/beancount/commit/ca980a5abbf44be4f6fb16f7731eae0870c9995a))
* **ledger:** add graph abstraction with pathfinding ([66249ae](https://github.com/robinvdvleuten/beancount/commit/66249aec075d2759a480c28ebf84c82cc4a5ca34))
* **ledger:** add reporting APIs with posting history ([0ea3f5b](https://github.com/robinvdvleuten/beancount/commit/0ea3f5b12e5c556abd901d00c629004270acac54))
* **ledger:** implement account hierarchy with balance aggregation ([aa6a6f2](https://github.com/robinvdvleuten/beancount/commit/aa6a6f2f78db6d18fdceb2a521059789d74d29b2))
* **ledger:** implement explicit commodity nodes in graph ([dcf0089](https://github.com/robinvdvleuten/beancount/commit/dcf0089b6f4c5ce9acfd48cc8313c91ab513723b))
* **ledger:** implement temporal price index with forward-fill semantics ([a065b5e](https://github.com/robinvdvleuten/beancount/commit/a065b5ec794a346f4ff64cc5eef286ea430dbc4c))
* **ledger:** support implicit posting amount inference ([598109d](https://github.com/robinvdvleuten/beancount/commit/598109d4cf7564ae55b093c2d81f75433ee7066d))
* support and preserve comma thousands separators ([#114](https://github.com/robinvdvleuten/beancount/issues/114)) ([2e39fee](https://github.com/robinvdvleuten/beancount/commit/2e39fee1f9b2cf4d5d4ba024d931cd43625d8e98))
* **web:** add read-only mode to UI and API ([bb390fe](https://github.com/robinvdvleuten/beancount/commit/bb390fe678a74506e8e61ad8fe8bde907dc9ea3a))


### Bug Fixes

* **parser:** handle blank lines between postings ([15c1063](https://github.com/robinvdvleuten/beancount/commit/15c1063a01a246588922351933553e35ea4e1992))
* **parser:** preserve blank lines after transaction postings ([fab5930](https://github.com/robinvdvleuten/beancount/commit/fab5930f64bd2088d0e90f39ff197a2633f8bc6b))
* **parser:** standardize token names to uppercase ([88dcb36](https://github.com/robinvdvleuten/beancount/commit/88dcb36280c219007c86e1c8c785a153be63b8a9))
* **web:** check return value of Fprint ([7f7b638](https://github.com/robinvdvleuten/beancount/commit/7f7b6389a7d43f013d3750c03ef395adc3638c01))
* **web:** correctly lowercase position properties when encoded to json ([310f47d](https://github.com/robinvdvleuten/beancount/commit/310f47dd1a6d72ae55caf3c525cbc044286a8f0a))
* **web:** start error marker at beginning of line ([b177d81](https://github.com/robinvdvleuten/beancount/commit/b177d8143e8b33e37d6f839c41ee538b10730102))

## [0.6.0](https://github.com/robinvdvleuten/beancount/compare/v0.5.0...v0.6.0) (2025-12-18)


### Features

* **ast:** attach string escape metadata and inline flag to AST nodes ([8bdd108](https://github.com/robinvdvleuten/beancount/commit/8bdd108b91983ddca01900b074fb225403baddec))
* **ast:** store inferred amounts directly on AST nodes ([7211e03](https://github.com/robinvdvleuten/beancount/commit/7211e03e516685c51c3bc9a57b910b627281d958))
* **lexer:** implement consistent token consumption ([bececd1](https://github.com/robinvdvleuten/beancount/commit/bececd15a6690de40760170da758c4dcbc62aacd))
* make codemirror aware of beancount syntax ([#79](https://github.com/robinvdvleuten/beancount/issues/79)) ([d7298fc](https://github.com/robinvdvleuten/beancount/commit/d7298fc69d830e81bab92df2b1e3e6a7162b2cbb))
* make codemirror popover colors consistent with overall theme ([6adaeb3](https://github.com/robinvdvleuten/beancount/commit/6adaeb36886e7eb4513f178074c0b19baf930174))
* **parser:** add UTF-8 validation to lexer ([9af4688](https://github.com/robinvdvleuten/beancount/commit/9af46889e218735501e0c71b14c909447208cb87))
* **parser:** add validation to reject invalid beancount syntax at parse time ([bf06702](https://github.com/robinvdvleuten/beancount/commit/bf0670240f8ff66c362e48c176ea958e2043f386))
* **parser:** improve string literal parsing with escapes and validation ([d873409](https://github.com/robinvdvleuten/beancount/commit/d873409f87b37e919497c74e2919ef56e38580af))
* **parser:** preserve original string escape information for round-trip formatting ([6bcfbc7](https://github.com/robinvdvleuten/beancount/commit/6bcfbc7b35bc9c7144ea6d7b915cf94dc4cfe079))
* **parser:** track escape sequences for round-trip formatting ([98b6c8b](https://github.com/robinvdvleuten/beancount/commit/98b6c8b8386a5b2ac884d602ff4e3eef6dbb1ef5))
* **web:** add account autocomplete with match-sorter ([90bdb2c](https://github.com/robinvdvleuten/beancount/commit/90bdb2cbd1df48af44ca4a52d432012bdb69f95e))
* **web:** add context-aware account autocomplete ([16e567e](https://github.com/robinvdvleuten/beancount/commit/16e567ea9cb32d6d922a31451a6f7566849acaf0))
* **web:** add GET /api/accounts endpoint ([0a6ce4f](https://github.com/robinvdvleuten/beancount/commit/0a6ce4fb723ab60392bdb6778d395e7717bb8d4f))


### Bug Fixes

* **formatter:** ensure idempotency by trimming leading and trailing blank lines ([5c33705](https://github.com/robinvdvleuten/beancount/commit/5c337056f889046e6fd27500b5b65841c009dcec))
* **formatter:** ensure idempotency by trimming trailing blank lines ([4d57fa1](https://github.com/robinvdvleuten/beancount/commit/4d57fa19061ffd06c1397a953419735ffc02143a))
* **formatter:** gracefully handle malformed input ([227895e](https://github.com/robinvdvleuten/beancount/commit/227895ee2cb30a38e24da561debef0250fd7d66c))
* **formatter:** resolve idempotency failure in Note/Document with inline metadata ([6c06ab8](https://github.com/robinvdvleuten/beancount/commit/6c06ab8d6a79e47fa73f2fbe026a56f93ebb2db0))
* **formatter:** respect Metadata.Inline flag for posting metadata ([c3c9abb](https://github.com/robinvdvleuten/beancount/commit/c3c9abbcad0317f9b1b10f0fb30045c1cd206579))
* **formatter:** skip directives with invalid dates to prevent malformed output ([c1008fe](https://github.com/robinvdvleuten/beancount/commit/c1008fe7568fe39fd257f8cbfcb0eccf69f991cd))
* **formatter:** skip raw tokens with newlines to preserve idempotency ([f552d07](https://github.com/robinvdvleuten/beancount/commit/f552d070d191f667d1fa8396b3ab09966d53f28a))
* **ledger:** reject invalid years ([48e35d2](https://github.com/robinvdvleuten/beancount/commit/48e35d21f4b898a2525375460ecd26c56c320134))
* metadata parsing and formatting bugs affecting idempotency ([b77a8e9](https://github.com/robinvdvleuten/beancount/commit/b77a8e940a9a9f1eddecccecc9e15f23381cca69))
* **parser:** correct position tracking for multi-line directives ([6e575be](https://github.com/robinvdvleuten/beancount/commit/6e575be6c562e0b1990e8af764dd8c1d6d9aa9df))
* **parser:** handle escaped backslash with escape chars correctly ([f59e7e5](https://github.com/robinvdvleuten/beancount/commit/f59e7e5e0affe867e36ef426a0b87cdf8f641d5d))
* **parser:** handle inline comments in transaction postings ([718444c](https://github.com/robinvdvleuten/beancount/commit/718444cff2bf4bddc71931117bace4a4513012a2))
* **parser:** require narration in transaction headers ([f59b5cf](https://github.com/robinvdvleuten/beancount/commit/f59b5cf923953770ebf1eded9ac4902c7717a20a))

## [0.5.0](https://github.com/robinvdvleuten/beancount/compare/v0.4.0...v0.5.0) (2025-11-03)


### Features

* add average time per item to all structured timer outputs ([0cea719](https://github.com/robinvdvleuten/beancount/commit/0cea719ccccf793c70944fb460c28f4bbcb4e4ee))
* serve interactive editor through `web` command ([#45](https://github.com/robinvdvleuten/beancount/issues/45)) ([7a58980](https://github.com/robinvdvleuten/beancount/commit/7a589805c6fad05accecc19b634969891eed0192))

### Bug Fixes

* **formatter:** auto-calculate currency column by default ([29076d4](https://github.com/robinvdvleuten/beancount/commit/29076d4c3a459081626dce6be07d23e0913b6dbd))

### Performance Improvements

* **parser:** add consistent string interning for memory savings ([a3ab4c9](https://github.com/robinvdvleuten/beancount/commit/a3ab4c981e0f20f30311d21bbffdd8cee6fd654a))
* **telemetry:** aggregate transaction validation timing for large files ([289198b](https://github.com/robinvdvleuten/beancount/commit/289198b6a98744717a6e5afa17fc04981ccfc2e4))

## [0.4.0](https://github.com/robinvdvleuten/beancount/compare/v0.3.0...v0.4.0) (2025-10-21)


### Features

* add source context to parse errors ([f032ef1](https://github.com/robinvdvleuten/beancount/commit/f032ef17b75ec4c69ef2795871de4e67ab9a4dea))
* add support for additional metadata value types ([195d6c5](https://github.com/robinvdvleuten/beancount/commit/195d6c519ddd9ca30ac1248dffbd9c0701b7089f))
* add support for expressions in amounts ([73d76de](https://github.com/robinvdvleuten/beancount/commit/73d76de93fce22b73ead5bdaae12d91f48b16df7))
* apply small optimizations to parser logic ([8b809cc](https://github.com/robinvdvleuten/beancount/commit/8b809cc3f8059da574e83cb435656d8663765c48))
* **cost:** add total cost syntax `{{…}}` ([5489905](https://github.com/robinvdvleuten/beancount/commit/548990504953bb2f893d413a3c8de80ee3fe2dac))
* **goreleaser:** add nfpms for linux packages ([c6b5545](https://github.com/robinvdvleuten/beancount/commit/c6b554557ad364bf61af9b8f5843b7d7cd94825c))
* **ledger:** add validators for costs/prices/directives ([8cf6a6c](https://github.com/robinvdvleuten/beancount/commit/8cf6a6c67faefe746d100447247fd7f5337bd855))
* **ledger:** implement merge-cost lots {*} functionality ([ddc409c](https://github.com/robinvdvleuten/beancount/commit/ddc409cced3ce92be22ed03d1e40aa5b01aea060))
* **ledger:** implement modern tolerance inference ([5dc2124](https://github.com/robinvdvleuten/beancount/commit/5dc21248275557c1bfe2ad9909ca0948f3b6cc1c))
* **ledger:** implement pad synthetic transactions ([e04a984](https://github.com/robinvdvleuten/beancount/commit/e04a98448647449e20385f1b3929d75745d6308f))
* **ledger:** implement validation/mutation separation ([0225cf6](https://github.com/robinvdvleuten/beancount/commit/0225cf60e7921e627bde2b85192dfd8ab1dc0598))
* make stdin default input when no filename provided ([5d417f6](https://github.com/robinvdvleuten/beancount/commit/5d417f698e2a6e50b2abd319a083fab0aac0f3dd))
* replaced participle with a custom recursive descent parser ([#43](https://github.com/robinvdvleuten/beancount/issues/43)) ([85d9ba2](https://github.com/robinvdvleuten/beancount/commit/85d9ba23a7927339ab7da499d7f6fb47b409e8be))
* support multiple values for option directives ([94f3bc6](https://github.com/robinvdvleuten/beancount/commit/94f3bc6bef42ac6feeaf77bab0d68860a6c07c96))
* **telemetry:** add µs precision and rounding indicators ([7e02668](https://github.com/robinvdvleuten/beancount/commit/7e02668b467eb983a9d6b3661a3d3ce61c8d746f))

### Bug Fixes

* add binary field to homebrew cask ([5f16baf](https://github.com/robinvdvleuten/beancount/commit/5f16baf4ed4909636f3e63346e24d14940a8b347))
* **ast:** add stable sort with line number tertiary key ([eeec8ba](https://github.com/robinvdvleuten/beancount/commit/eeec8ba1478dc1830f977006e1c7796954cff1c0))
* correctly resolve binaries when testing on windows ([bea1a35](https://github.com/robinvdvleuten/beancount/commit/bea1a35a5d8827034495a3ccb7789f28385cc298))
* **goreleaser:** package executable not archive ([5835fcb](https://github.com/robinvdvleuten/beancount/commit/5835fcb02f403dc1ee03575d9787bc3cd030915c))
* match formatting defaults with bean-format ([be80e43](https://github.com/robinvdvleuten/beancount/commit/be80e431e78f02c5de5c29d2325ae26e812e1476))
* **parser:** report error at the end of the number token ([f852036](https://github.com/robinvdvleuten/beancount/commit/f852036537bcf06e7c6a3c257889c95902197112))
* support Unicode characters in account names ([b8ff156](https://github.com/robinvdvleuten/beancount/commit/b8ff1560f91cd0a111c3ff4e2e00015d862bbeda))
* **telemetry:** correct hierarchy and timer lifecycle ([cb3e100](https://github.com/robinvdvleuten/beancount/commit/cb3e1007d92a2d1aedc91e82eb9d1ba18c8816c1))

## [0.3.0](https://github.com/robinvdvleuten/beancount/compare/v0.2.0...v0.3.0) (2025-10-17)


### Features

* add context for cancellation support ([7f4f14c](https://github.com/robinvdvleuten/beancount/commit/7f4f14c06e47b6177fff16d72d2fe5bcb9ecda5a))
* add timing telemetry with --telemetry flag ([5902a9f](https://github.com/robinvdvleuten/beancount/commit/5902a9f5ef53b8d00d71f89d134ad57d72f4a8fb))
* **ast:** add builder functions with functional options ([39fa281](https://github.com/robinvdvleuten/beancount/commit/39fa2815f1b54100468893969d6770ca3c4a03c0))
* expose ast types through `ast/` package ([930f0d6](https://github.com/robinvdvleuten/beancount/commit/930f0d64a0d47d5b5177b1f8d7b553bfea28193b))
* make error formatting consistent ([c607c41](https://github.com/robinvdvleuten/beancount/commit/c607c419e2ed8cdd0b4938ddd865740a48ce09c7))
* **telemetry:** make flag global, add to format ([785b531](https://github.com/robinvdvleuten/beancount/commit/785b5317ca48fca704ef3b532e5dc092677f30fa))

## [0.2.0](https://github.com/robinvdvleuten/beancount/compare/v0.1.0...v0.2.0) (2025-10-17)


### Features

* add support for `custom` directive ([54d352a](https://github.com/robinvdvleuten/beancount/commit/54d352a4b2c87d5864fb41b8eb403ecdc492eebf))
* add support for `plugin` directive ([8112921](https://github.com/robinvdvleuten/beancount/commit/81129219846416a5927b1559c501b39a3e808288))
* add support for pushtag/poptag and pushmeta/popmeta ([d318c52](https://github.com/robinvdvleuten/beancount/commit/d318c5267e81b903b974a73a4a894eba89e6f7c4))
* add transaction context to account errors ([d627eb6](https://github.com/robinvdvleuten/beancount/commit/d627eb6cb0501a85c427295d0e6d8137863f6d95))
* add transaction context to errors ([b582421](https://github.com/robinvdvleuten/beancount/commit/b582421128678c18112f543757a07136bfdc44a3))
* align currencies correctly regardless of character type ([66f99e5](https://github.com/robinvdvleuten/beancount/commit/66f99e5e7f81053eda31bb3c013936ec3722734f))
* initial ledger functionality ([#42](https://github.com/robinvdvleuten/beancount/issues/42)) ([fd495c6](https://github.com/robinvdvleuten/beancount/commit/fd495c6b761a517f0ff685e61dcb53ff2d212396))
* make parser accept quoted string as booking method ([3a369b9](https://github.com/robinvdvleuten/beancount/commit/3a369b9227359824ddb732c020889168215e9123))
* pass short commit when building ([12725d1](https://github.com/robinvdvleuten/beancount/commit/12725d105b3916f496564d9e68ce62e979e2c116))
* resolve files from include directives ([2fdd162](https://github.com/robinvdvleuten/beancount/commit/2fdd162099299ceed86e000b727a19afea4b9607))
* show usage instead of error ([836c496](https://github.com/robinvdvleuten/beancount/commit/836c496aefa185f9d74697ff5b723c510a5c9bce))
* sign checksums when releasing ([17176a6](https://github.com/robinvdvleuten/beancount/commit/17176a6c8456ee58560043512bed934e64330791))
* update formatter with additional directives ([15e2495](https://github.com/robinvdvleuten/beancount/commit/15e24957c0e706ce3d6fe9e6c4fcf748728d547a))

### Bug Fixes

* allow transactions on account close date ([d98d576](https://github.com/robinvdvleuten/beancount/commit/d98d576271b37ad12c5386b95573ab4c587d9fbf))
* correctly handle slashes on windows ([22502eb](https://github.com/robinvdvleuten/beancount/commit/22502eb27db02a3efa8486d496643b7aa9e1bd84))
* detect empty cost specs in weight calculation ([6c60c44](https://github.com/robinvdvleuten/beancount/commit/6c60c44583f6b9afb04d988216804f753386234c))
* infer costs for empty cost spec augmentations ([1ea35e1](https://github.com/robinvdvleuten/beancount/commit/1ea35e1b59734ca24fca485d14eed40455f654e6))
* preserve original spacing when formatting ([d00e75f](https://github.com/robinvdvleuten/beancount/commit/d00e75f4ab85497dc661d84a5a69cd5120aa1e16))
* update test for close date behavior ([d784e27](https://github.com/robinvdvleuten/beancount/commit/d784e27e283b25bbd7071668710ff5d56107f208))

## [0.1.0](https://github.com/robinvdvleuten/beancount/compare/8d61b14762d3ac59f747c474adbd4561d3b7a105...v0.1.0) (2025-10-17)


### Features

* account validation through switch statement ([fc7d37b](https://github.com/robinvdvleuten/beancount/commit/fc7d37b9e0c461641facc4928b91b7ffa39c5a88))
* add merge cost specification ([13a6845](https://github.com/robinvdvleuten/beancount/commit/13a68458a9781652e723a72046d73cbc646446d5))
* Add support for `document` directive ([4808d39](https://github.com/robinvdvleuten/beancount/commit/4808d393d118dab8280c8fa034ccb93a6e2e55fb))
* Add support for `event` directive ([356fbe1](https://github.com/robinvdvleuten/beancount/commit/356fbe16a3658dc507dcad3352911608cc5b9b4b))
* Add support for `note` directive ([482b970](https://github.com/robinvdvleuten/beancount/commit/482b9705239f2d1596dd2edc70431aed6dd5ba08))
* Add support for `pad` directive ([1969c96](https://github.com/robinvdvleuten/beancount/commit/1969c96e30431cb01039e874719e0f6ff453704b))
* Add support for `price` directive ([30023f4](https://github.com/robinvdvleuten/beancount/commit/30023f4e62eb0919756e97f071566b5b159d7ee8))
* add support for cost with date syntax ([90e9b58](https://github.com/robinvdvleuten/beancount/commit/90e9b5811a4cc61feb3305040c9da0bafbc7e43d))
* add support for empty costs (`{}`) ([b0f190d](https://github.com/robinvdvleuten/beancount/commit/b0f190d6b565fb4eda4d9aa84bc71caf6d7478a5))
* add support for formatting beancount files ([#41](https://github.com/robinvdvleuten/beancount/issues/41)) ([aefe473](https://github.com/robinvdvleuten/beancount/commit/aefe47372db68c942d3348d8b6f30ae56ad51d16))
* Add support for include directives ([629f3fe](https://github.com/robinvdvleuten/beancount/commit/629f3fed157b6e6cd2b6fc71336f39a65d75c42b))
* add support for links in transactions ([#40](https://github.com/robinvdvleuten/beancount/issues/40)) ([5975259](https://github.com/robinvdvleuten/beancount/commit/59752596c0740c29e50b10aab3d2eb1d3d1b4e14))
* add support for tags ([138df65](https://github.com/robinvdvleuten/beancount/commit/138df653c8a192542cf97bf2cd14c3aa357d790d))
* attach text labels to cost basis ([e7ecd05](https://github.com/robinvdvleuten/beancount/commit/e7ecd05a580e3f5f6a8c00e26c85b15f9fa7ab33))
* Capture dates as Date structs ([c46e387](https://github.com/robinvdvleuten/beancount/commit/c46e387606f72185f66f484ce563ad46ab34e5df))
* Define all possible directives as structs ([8d61b14](https://github.com/robinvdvleuten/beancount/commit/8d61b14762d3ac59f747c474adbd4561d3b7a105))
* directly parse date from guaranteed format ([1703578](https://github.com/robinvdvleuten/beancount/commit/17035788a7e934b15423e7c0627e9df7992cf4eb))
* expose version information through CLI ([3d23350](https://github.com/robinvdvleuten/beancount/commit/3d233505573beed2433e5a7322026f0103b4da72))
* Let kong handle reading the file’s content ([8dfe1af](https://github.com/robinvdvleuten/beancount/commit/8dfe1af19dabe682f53d6c9a9b503e76a969966e))
* Make account name parsing stricter ([5edd9be](https://github.com/robinvdvleuten/beancount/commit/5edd9beb9302777a3b90cda234e2319931de5b73))
* Move parser to subpackage ([2c12ca8](https://github.com/robinvdvleuten/beancount/commit/2c12ca83cd4db64baccc485bca9bb6ecb7ede22a))
* remove prefixes from links and tags ([550432a](https://github.com/robinvdvleuten/beancount/commit/550432a8b3c36f97a2b2c8dffbc3fa7d8ffe0e3c))
* remove unnecessary time.Time() conversion ([2f3d2a8](https://github.com/robinvdvleuten/beancount/commit/2f3d2a83a070aff81c6b66e900f9e835150c74d2))
* require at least go 1.24 ([137f238](https://github.com/robinvdvleuten/beancount/commit/137f238ca5451c88c9dac5d2a563a0857aa250ad))
* Reuse Amount struct to define price on posting ([dd4c6be](https://github.com/robinvdvleuten/beancount/commit/dd4c6be8e588b56e64851e7e296df36cd725c3a0))
* simplified account regex pattern ([b4e3381](https://github.com/robinvdvleuten/beancount/commit/b4e3381d183532ae13848904963aa4971854a77b))
* skip characters from guaranteed format ([abf4a47](https://github.com/robinvdvleuten/beancount/commit/abf4a470c99e2f1eb4942f96f1722f14bc400edf))
* skip sorting if already sorted ([04e25c8](https://github.com/robinvdvleuten/beancount/commit/04e25c8546edf5e66642ab65f1229c95056418a5))
* Sort directives by date while checking ([738ca7c](https://github.com/robinvdvleuten/beancount/commit/738ca7cdc377bae642797129ff0e61ed041ac704))

### Bug Fixes

* Check error return value of Capture() call ([438577d](https://github.com/robinvdvleuten/beancount/commit/438577d1e975a9d967b7f097c4b8a1072c211dd3))
* Make constraint currencies on open directive optional ([2ef88a8](https://github.com/robinvdvleuten/beancount/commit/2ef88a8ac96947ec5be0e83ccb396223536af64d))
