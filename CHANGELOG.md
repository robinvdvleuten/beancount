# Changelog

All notable changes to this project will be documented in this file.

## [0.17.1](https://github.com/robinvdvleuten/beancount/compare/v0.17.0...v0.17.1) (2026-10-08)


### Bug Fixes

* **formatter:** keep comments before a body tag line in place ([a8085d0](https://github.com/robinvdvleuten/beancount/commit/a8085d03a1c2fe42f88f6c5daf86b8d8bcd8c2e4))
* **loader:** match include globs as Python's fnmatch does ([f9d4a40](https://github.com/robinvdvleuten/beancount/commit/f9d4a406b3692fd0ccc4d84af16560087073d79f))
* **parser:** accept a directive's keyword right after its date ([017cc55](https://github.com/robinvdvleuten/beancount/commit/017cc55de04a5a8abed01b193f6ebcb0df9d11cd))
* **parser:** read metadata after comments and a tag line in a body ([4bcfb67](https://github.com/robinvdvleuten/beancount/commit/4bcfb670465aee0b61f7b94b0d7c76096a212490))
* **parser:** report a signed malformed number while recovering ([90aed34](https://github.com/robinvdvleuten/beancount/commit/90aed340f5997d8a33434ec606d5def616fcbf60))
* **query:** negate possign for an account under no account type ([d57b449](https://github.com/robinvdvleuten/beancount/commit/d57b449d0a2e567f376c73014d7ba724142f52d1))

## [0.17.0](https://github.com/robinvdvleuten/beancount/compare/v0.16.0...v0.17.0) (2026-10-06)


### ⚠ BREAKING CHANGES

* **formatter:** formatter no longer exports WithPreserveComments, WithPreserveBlanks, WithIndentation, WithStringEscapeStyle, the StringEscapeStyle type and its EscapeStyleNone, EscapeStyleCStyle and EscapeStyleOriginal constants, the Formatter fields PreserveComments, PreserveBlanks, Indentation and StringEscapeStyle, or the constants DefaultIndentation, MinimumSpacing and DateWidth.
* the Message field of parser.ParseError, ast.PushPopError and config.DeprecatedOptionError is now Msg, and the config errors and parser.ParseError no longer implement MarshalJSON.
* **formatter:** Format requires the source the AST was parsed from, and WithStringEscapeStyle has no effect.
* **ledger:** package ledger exports only what cli, web, query, printer, ledgerload and its external tests use.

### Features

* **ledger:** let errors.As match a balance mismatch as a *Diagnostic ([601f5f7](https://github.com/robinvdvleuten/beancount/commit/601f5f702a738a917adb36885a213c3a75cad306)), closes [#677](https://github.com/robinvdvleuten/beancount/issues/677)


### Bug Fixes

* **cli:** print an error beancount raises with no entry on its own ([ff205e5](https://github.com/robinvdvleuten/beancount/commit/ff205e56891453f54f9b463581be816fa0408584)), closes [#686](https://github.com/robinvdvleuten/beancount/issues/686)
* **config:** reject a name_* option that is not a root account name ([4cc0d26](https://github.com/robinvdvleuten/beancount/commit/4cc0d26adb23b5546aa747f9fdaf090eb848dc37)), closes [#666](https://github.com/robinvdvleuten/beancount/issues/666)
* **deps:** override seroval to 1.6 for GHSA-p6vx-979v-rg4c ([fd557b1](https://github.com/robinvdvleuten/beancount/commit/fd557b1441a1b8d06bc9c44d55bf7a5b210f02b6))
* **formatter:** copy a posting without units as written ([07cedbd](https://github.com/robinvdvleuten/beancount/commit/07cedbdbe7e20e102731fa00ffeffe3abba9de13)), closes [#661](https://github.com/robinvdvleuten/beancount/issues/661)
* **formatter:** measure aligned widths in code points, as bean-format does ([36d8bb3](https://github.com/robinvdvleuten/beancount/commit/36d8bb30fd840a9cea324e43a9ee482441f1856b)), closes [#676](https://github.com/robinvdvleuten/beancount/issues/676)
* **formatter:** read lines with bean-format's own patterns ([d0b412e](https://github.com/robinvdvleuten/beancount/commit/d0b412eac4a00c5356ca3fc46f2ada575f26544c)), closes [#664](https://github.com/robinvdvleuten/beancount/issues/664)
* **ledger:** book a reduction that empties a lot with the lot's number ([2368699](https://github.com/robinvdvleuten/beancount/commit/2368699c58064433c9715c6fc41ac07178026174))
* **ledger:** book missing units at a zero price in their own currency as the residual ([d603811](https://github.com/robinvdvleuten/beancount/commit/d60381186c3fb5b759120931623fa027e52493f4))
* **ledger:** check a balance assertion on an account never opened ([a03dfbf](https://github.com/robinvdvleuten/beancount/commit/a03dfbf225aa1b8e8a419e05f6a0148e9d3fde1d))
* **ledger:** constrain postings by the last open naming currencies ([698a5ae](https://github.com/robinvdvleuten/beancount/commit/698a5aedc79f4bd1fa2e82dd2f6ddaa5f22281fc)), closes [#690](https://github.com/robinvdvleuten/beancount/issues/690)
* **ledger:** count a reduction's missing price once per lot it is booked against ([3d0e5cd](https://github.com/robinvdvleuten/beancount/commit/3d0e5cd4ad204110737b24f867fdaac410596328))
* **ledger:** count accounts never opened in the balance tree ([021310d](https://github.com/robinvdvleuten/beancount/commit/021310daf3d8f4573695569431057e481195acee)), closes [#672](https://github.com/robinvdvleuten/beancount/issues/672)
* **ledger:** count an account's subaccounts in a balance assertion ([f690c01](https://github.com/robinvdvleuten/beancount/commit/f690c01b73cc5ce24adacd2627b922151cd3f98a))
* **ledger:** count the amount of a dropped metadata key towards display precision ([4bfcf50](https://github.com/robinvdvleuten/beancount/commit/4bfcf50039b364866e462f8f597b68d98ee1a1c3))
* **ledger:** count the amounts of dropped directives towards display precision ([c870766](https://github.com/robinvdvleuten/beancount/commit/c8707666d6618b1f046cd48de433668188e4ab41)), closes [#693](https://github.com/robinvdvleuten/beancount/issues/693)
* **ledger:** count the zero per-unit part of a total cost towards its tolerance ([440a7bb](https://github.com/robinvdvleuten/beancount/commit/440a7bb444315066f9b63dbae916223ef8f2b1b7))
* **ledger:** divide missing units by a price in their own currency ([4449397](https://github.com/robinvdvleuten/beancount/commit/4449397e2875559a106257994c2c00212e09e07a)), closes [#701](https://github.com/robinvdvleuten/beancount/issues/701)
* **ledger:** do not count a booking against a lot of its own sign as a reduction ([2244914](https://github.com/robinvdvleuten/beancount/commit/2244914c6d1c53a31b1e30345b9b52b336d27d69))
* **ledger:** give a lot the cost number of its latest addition ([a241bc4](https://github.com/robinvdvleuten/beancount/commit/a241bc43f01ec7a133c0d73bb06c34aae5cfc5d3))
* **ledger:** give a number-less cost spec its Currency group's currency ([4e7fadd](https://github.com/robinvdvleuten/beancount/commit/4e7fadd864db7abf25d51670024bcca18bcbd737)), closes [#655](https://github.com/robinvdvleuten/beancount/issues/655)
* **ledger:** give a number-only price its Currency group's currency ([5c94505](https://github.com/robinvdvleuten/beancount/commit/5c945059f63c8d1db3540564d421acc08ee3e929)), closes [#651](https://github.com/robinvdvleuten/beancount/issues/651)
* **ledger:** give a zero quotient the exponent Python's division gives it ([936bbb8](https://github.com/robinvdvleuten/beancount/commit/936bbb87487d5568ab5843d338fddaedd1c508d7))
* **ledger:** give an open with an invalid booking method the option in effect ([c6bbce1](https://github.com/robinvdvleuten/beancount/commit/c6bbce14e308cdf42b9f1d93b12985d6de808c13)), closes [#696](https://github.com/robinvdvleuten/beancount/issues/696)
* **ledger:** interpolate a cost from a zero residual as the plain zero ([050420e](https://github.com/robinvdvleuten/beancount/commit/050420e7f46c6d8a235f1e53e4670af3f838c495))
* **ledger:** interpolate missing units inside total braces ([1aaba4c](https://github.com/robinvdvleuten/beancount/commit/1aaba4ce4f933712706e96418e9a7666424444b0)), closes [#652](https://github.com/robinvdvleuten/beancount/issues/652)
* **ledger:** keep a posting whose total price is dropped from spreading over groups ([098ebdd](https://github.com/robinvdvleuten/beancount/commit/098ebddcb33b568b420224f943ad6c15fd0d3ffa)), closes [#702](https://github.com/robinvdvleuten/beancount/issues/702)
* **ledger:** keep a residual's trailing zeros in "Transaction does not balance" ([44a5666](https://github.com/robinvdvleuten/beancount/commit/44a56668b692b2219ea557d20b7e4269b21489f3)), closes [#682](https://github.com/robinvdvleuten/beancount/issues/682)
* **ledger:** keep one value for a metadata key written twice, as beancount does ([35165b1](https://github.com/robinvdvleuten/beancount/commit/35165b1a74de9df7d9dbc49d4f0175aa194be9f9)), closes [#697](https://github.com/robinvdvleuten/beancount/issues/697)
* **ledger:** keep the exponent of an interpolated price ([6942a01](https://github.com/robinvdvleuten/beancount/commit/6942a0174af69076454bfe2a5d6db001a7fc37f4))
* **ledger:** keep trailing zeros in "Cost is negative" and balance mismatches ([2b3f063](https://github.com/robinvdvleuten/beancount/commit/2b3f0637f602f45695fbe5f8f91ddab2a2bfffd0)), closes [#688](https://github.com/robinvdvleuten/beancount/issues/688)
* **ledger:** leave a cost without units its units currency unresolved ([62f89ea](https://github.com/robinvdvleuten/beancount/commit/62f89ea8b2910139168752b2e11fb63a5701ce35)), closes [#689](https://github.com/robinvdvleuten/beancount/issues/689)
* **ledger:** leave a price that is zero per unit out of the currencies a transaction names ([2ea0b04](https://github.com/robinvdvleuten/beancount/commit/2ea0b04830f5cf9cba5b43dcc93c1661d7c7775f))
* **ledger:** leave a total cost without units to categorization ([9bc0c72](https://github.com/robinvdvleuten/beancount/commit/9bc0c72f9de61f50ebd0a75c54bff91f8a58c28c))
* **ledger:** leave interpolated postings out of the residual's tolerance ([9e92e2c](https://github.com/robinvdvleuten/beancount/commit/9e92e2caa46ccd2b50eca6bff8d636b626781154))
* **ledger:** make an account active again on a duplicate open after its close ([a06162f](https://github.com/robinvdvleuten/beancount/commit/a06162f5c7948bbed3d3a9228ffc7bb183b0191d))
* **ledger:** match a reduction against lots of either sign ([324899f](https://github.com/robinvdvleuten/beancount/commit/324899f58c79eb3a8b2fb0cc5cc454cb55e5af61)), closes [#654](https://github.com/robinvdvleuten/beancount/issues/654)
* **ledger:** name the asserted amount in a padding as beancount prints its number ([0d4478f](https://github.com/robinvdvleuten/beancount/commit/0d4478fb0489de7381ea1bd7ddc6d5c8d8ef0abd))
* **ledger:** number an open auto_accounts inserts among every account used ([e379760](https://github.com/robinvdvleuten/beancount/commit/e379760799cd300e64b0523b8622d348f81e932a))
* **ledger:** order residuals as beancount's Position.sortkey does ([ffbcba4](https://github.com/robinvdvleuten/beancount/commit/ffbcba4e91c984bcf54d61b3e64383077b35da96)), closes [#685](https://github.com/robinvdvleuten/beancount/issues/685)
* **ledger:** plan paddings from the booked ledger before any assertion is checked ([547c344](https://github.com/robinvdvleuten/beancount/commit/547c344cd2aeeefcf9a86ed571698ab33ff4155f))
* **ledger:** report a missing price on units held at cost ([40c5d09](https://github.com/robinvdvleuten/beancount/commit/40c5d09cf8542918a66c81605c4a35f504fa14c1)), closes [#656](https://github.com/robinvdvleuten/beancount/issues/656)
* **ledger:** report a negative cost on each lot a reduction is booked against ([52d84d8](https://github.com/robinvdvleuten/beancount/commit/52d84d8823196b17940eeb706ec3c28bb8cb948e))
* **ledger:** report a price in another currency than the cost ([ba16c67](https://github.com/robinvdvleuten/beancount/commit/ba16c6705388a027d78eb75b1cd69495f288c0d8)), closes [#699](https://github.com/robinvdvleuten/beancount/issues/699)
* **ledger:** report a repeated metadata key only for another value object ([7169c0b](https://github.com/robinvdvleuten/beancount/commit/7169c0b76ff5b7fced4ee32650121f76047c1d85)), closes [#706](https://github.com/robinvdvleuten/beancount/issues/706)
* **ledger:** report every close after an account's first as a duplicate ([820f7fd](https://github.com/robinvdvleuten/beancount/commit/820f7fd7fb8ee2990965809a4e8a302c210dddbb))
* **ledger:** report interpolation errors as InterpolationError ([491132e](https://github.com/robinvdvleuten/beancount/commit/491132ecc645103991b671603c508dc8c361d616))
* **ledger:** resolve the units currency of a posting with a price ([f94b56b](https://github.com/robinvdvleuten/beancount/commit/f94b56b2efba50368d873ee4e7bd7de5afc01285)), closes [#648](https://github.com/robinvdvleuten/beancount/issues/648)
* **ledger:** spread a total cost over the units as compute_cost_number does ([aa03a42](https://github.com/robinvdvleuten/beancount/commit/aa03a42833fa907e30a9bd3c250a3ba3a7453dfe))
* **ledger:** sum a balance assertion's subtree as one inventory ([0137496](https://github.com/robinvdvleuten/beancount/commit/0137496b0c71fd63ab6006375bb0a50310ddb703))
* **ledger:** sum Closed balances in beancount's order ([384449e](https://github.com/robinvdvleuten/beancount/commit/384449e1ebcd3ddf1945ea0b719c4f648645ded3)), closes [#670](https://github.com/robinvdvleuten/beancount/issues/670)
* **ledger:** sum the booked residual in posting order ([5466b75](https://github.com/robinvdvleuten/beancount/commit/5466b751851506a590c71ca41d187063eaf23cfc))
* **ledger:** take a number-only price's currency from the account's lots ([d3fc964](https://github.com/robinvdvleuten/beancount/commit/d3fc964f1c9f1595ebac15ea6594fb79e9c4292c)), closes [#653](https://github.com/robinvdvleuten/beancount/issues/653)
* **ledger:** treat an empty string as a plugin's configuration ([935092e](https://github.com/robinvdvleuten/beancount/commit/935092e149f578c2e925ccd1cd19a77ae3307946))
* **ledger:** weigh a reduction at the number its lot holds ([5f98fb3](https://github.com/robinvdvleuten/beancount/commit/5f98fb38d11564db6041a330a507acf43348e726))
* **ledger:** weigh an interpolated price at units times price ([f2ad286](https://github.com/robinvdvleuten/beancount/commit/f2ad28683767fa2909ac96b1a7f1a28b0d6e3e4f)), closes [#649](https://github.com/robinvdvleuten/beancount/issues/649)
* **ledger:** weigh nothing for missing units left out at a compound cost ([635c163](https://github.com/robinvdvleuten/beancount/commit/635c16351263d29081a6e4c579553934d9bc6112)), closes [#707](https://github.com/robinvdvleuten/beancount/issues/707)
* **ledger:** weigh units interpolated at a compound cost per unit ([46a6bae](https://github.com/robinvdvleuten/beancount/commit/46a6baec0bd1ec13c9fb13e031c6266a724bcb99)), closes [#650](https://github.com/robinvdvleuten/beancount/issues/650)
* **ledger:** word check's messages without Python's artifacts ([279c158](https://github.com/robinvdvleuten/beancount/commit/279c158f75997452d9098edf07af919849d334f3))
* **ledger:** write an interpolated price as Python writes its quotient ([4d5e4bb](https://github.com/robinvdvleuten/beancount/commit/4d5e4bbb9a62e603f017e96da9e1a4e9a6b47de2))
* **lexer:** read a lone carriage return as whitespace ([89d771a](https://github.com/robinvdvleuten/beancount/commit/89d771aeaecc1b0013ea6b96d987d4144d57a09e)), closes [#663](https://github.com/robinvdvleuten/beancount/issues/663)
* **parser:** blame a string continuing a header on the line it starts on ([502c2eb](https://github.com/robinvdvleuten/beancount/commit/502c2ebf85551a923cd6f9dbe039199b33d03fb3))
* **parser:** blame an error on a string spanning lines on the line it ends on ([0711583](https://github.com/robinvdvleuten/beancount/commit/07115834abd71ea5de0da5716ee4aee48d58444f))
* **parser:** check an account's root on every line against the names in effect ([777628c](https://github.com/robinvdvleuten/beancount/commit/777628cf0ef104a52b350469cb5a924926f3e43e)), closes [#691](https://github.com/robinvdvleuten/beancount/issues/691)
* **parser:** count a continued header's error from the end of its line ([71b6f15](https://github.com/robinvdvleuten/beancount/commit/71b6f15611eb8a489d16b5e56f9ad1354f262874))
* **parser:** count a dropped transaction's cost numbers towards display precision ([f8ab392](https://github.com/robinvdvleuten/beancount/commit/f8ab3929af6b198da5410df72c95f21f8cfbafcf))
* **parser:** count a lone date's error from after the date ([91b8194](https://github.com/robinvdvleuten/beancount/commit/91b8194bbfa89444dbe5aea725396400cba3134f))
* **parser:** count the tokens after an erroring line that ends in a comment ([a379554](https://github.com/robinvdvleuten/beancount/commit/a3795546a66da40fb4d1f1dcc3401ead41050f2a))
* **parser:** count the tokens before a string spanning lines up to where it starts ([cc58f18](https://github.com/robinvdvleuten/beancount/commit/cc58f18cec2089edabc1b9243e6ad355bd9b359d))
* **parser:** drop a directive holding an account word beancount's lexer rejects ([aa1c6ef](https://github.com/robinvdvleuten/beancount/commit/aa1c6ef3c480e5c500372617d7f4b64602e01145)), closes [#698](https://github.com/robinvdvleuten/beancount/issues/698)
* **parser:** drop a directive whose account is followed by a colon ([455c317](https://github.com/robinvdvleuten/beancount/commit/455c317f793cfa90edda93697db2b9ade3733e24))
* **parser:** end a directive's body at a blank line ([5cb4994](https://github.com/robinvdvleuten/beancount/commit/5cb49944489fa186f10b8d1bc580f216ff23eec8)), closes [#662](https://github.com/robinvdvleuten/beancount/issues/662)
* **parser:** end a parenthesis that opens no number expression at itself ([fa4b790](https://github.com/robinvdvleuten/beancount/commit/fa4b79063c9db1e6a9cf5f53bbf906aab12b201f))
* **parser:** end a string token's line where a lone CR breaks it ([f37e53a](https://github.com/robinvdvleuten/beancount/commit/f37e53ac296fec674fb862eb30c975e6870f69fb))
* **parser:** end the recovery of a header read over several lines at its last line ([36e20f7](https://github.com/robinvdvleuten/beancount/commit/36e20f722e91b6599199a74adc19b950c458822d))
* **parser:** fall back to an included file's booking_method on an invalid method ([da47b38](https://github.com/robinvdvleuten/beancount/commit/da47b389d8f0575d5ebdf6ac5f5a8f3348b06966)), closes [#700](https://github.com/robinvdvleuten/beancount/issues/700)
* **parser:** keep a posting's metadata that follows a tag or link line ([af563bf](https://github.com/robinvdvleuten/beancount/commit/af563bff2502581d2cc6b70a5a0badd8ff9d18b6))
* **parser:** keep a transaction with a tag or link line after a posting ([a954c07](https://github.com/robinvdvleuten/beancount/commit/a954c0735c540cc4cd33a1d0702aeb072bc692da)), closes [#694](https://github.com/robinvdvleuten/beancount/issues/694)
* **parser:** keep what a directive read across a body comment ([3ce15c1](https://github.com/robinvdvleuten/beancount/commit/3ce15c11eb83d98a7bd31aa23bc86144411b9467))
* **parser:** leave an amount read past a header's line out of display precision ([6f97e7d](https://github.com/robinvdvleuten/beancount/commit/6f97e7d43eb0926e0fde1a295da1d54359ce1cbc))
* **parser:** lex a quote no string closes as an invalid token, as beancount does ([962b3b6](https://github.com/robinvdvleuten/beancount/commit/962b3b63a7c35d0a64e98d191f6863fb0175d52d)), closes [#692](https://github.com/robinvdvleuten/beancount/issues/692)
* **parser:** lex a sign before a date as a token of its own ([875a869](https://github.com/robinvdvleuten/beancount/commit/875a869a985878d1e946185cac279f2b4fd04191))
* **parser:** read a custom directive's account value as an account ([9edae9f](https://github.com/robinvdvleuten/beancount/commit/9edae9f0a5bc9e0326b119b9be28edaba92a52a8))
* **parser:** read a date as beancount's lexer does, parts of any width ([0ca5c48](https://github.com/robinvdvleuten/beancount/commit/0ca5c48519cde819f8d8b27ffb29b7a80dfe7f8c))
* **parser:** read a transaction with too many strings to its end before dropping it ([18ade19](https://github.com/robinvdvleuten/beancount/commit/18ade19775c80004eaadc557985b6d3cb8af885c))
* **parser:** read an account and a currency as far as beancount's lexer does ([7e2d61f](https://github.com/robinvdvleuten/beancount/commit/7e2d61ffb69607bf27cf9c1bfac510e42109f8b3))
* **parser:** read leading whitespace holding a lone \r as no indent ([43cb4ef](https://github.com/robinvdvleuten/beancount/commit/43cb4efe98ddfc3c513ade2cd69b4ef3ae365eef)), closes [#680](https://github.com/robinvdvleuten/beancount/issues/680)
* **parser:** read the deprecated pipe between payee and narration as beancount does ([f94c7b0](https://github.com/robinvdvleuten/beancount/commit/f94c7b07521abf584e48bf12b88b5aa357fa0859)), closes [#695](https://github.com/robinvdvleuten/beancount/issues/695)
* **parser:** recover from invalid UTF-8 and control characters ([1f870f3](https://github.com/robinvdvleuten/beancount/commit/1f870f34579f6b73325ebf9ceaf5ca066ce9f3d2)), closes [#668](https://github.com/robinvdvleuten/beancount/issues/668)
* **parser:** reject a transaction with too many strings without syntax recovery ([f741c1c](https://github.com/robinvdvleuten/beancount/commit/f741c1cdc10bdd3d77f6b15784ba0d3aa614ef12))
* **parser:** report a link written as a metadata value as a syntax error ([463dbdb](https://github.com/robinvdvleuten/beancount/commit/463dbdbff0ca7f200e0ea800c1ec838cc5bfb258))
* **parser:** report a metadata line in column 1 as a syntax error ([f1fb5b9](https://github.com/robinvdvleuten/beancount/commit/f1fb5b9965955bbea121598e0dda0f0ba70ebea9)), closes [#703](https://github.com/robinvdvleuten/beancount/issues/703)
* **parser:** report a posting's grammar errors when its transaction is dropped ([74b0511](https://github.com/robinvdvleuten/beancount/commit/74b05117a6d8ea299e958d43a9223d65faddb227)), closes [#705](https://github.com/robinvdvleuten/beancount/issues/705)
* **parser:** report a word the lexer rejects at line start as an invalid token ([9aec00c](https://github.com/robinvdvleuten/beancount/commit/9aec00cf40a1711e696fc3523569f65c84e5e25d)), closes [#681](https://github.com/robinvdvleuten/beancount/issues/681)
* **parser:** report an indented line at top level where its indentation is ([a07e0eb](https://github.com/robinvdvleuten/beancount/commit/a07e0eba94d01edfd707414c2967f180f08c3806))
* **parser:** report more than one value after a pushmeta as a syntax error ([7e92a8a](https://github.com/robinvdvleuten/beancount/commit/7e92a8a318ae9a8a56fe33208687db54031e8150))
* **parser:** resume after a syntax error at a token that can start a declaration ([962ddd6](https://github.com/robinvdvleuten/beancount/commit/962ddd64fe121e80e7a28a74a88eb2e39f8d4f5b)), closes [#679](https://github.com/robinvdvleuten/beancount/issues/679)
* **parser:** suppress syntax errors that follow another too closely ([01d466a](https://github.com/robinvdvleuten/beancount/commit/01d466acdf705beffc311756d353bad965b5958c)), closes [#667](https://github.com/robinvdvleuten/beancount/issues/667)
* **parser:** take a backslash before any character in a string ([c807eb0](https://github.com/robinvdvleuten/beancount/commit/c807eb09f00e34f6806a3c89c5cd94aeff429793))
* **parser:** take a plugin's configuration only from the plugin's own line ([eab9297](https://github.com/robinvdvleuten/beancount/commit/eab9297bfad8bb6b84c45a604ba998d8f01ba928))
* **parser:** take back a too-many-strings error when the transaction is broken off ([6c3b7bd](https://github.com/robinvdvleuten/beancount/commit/6c3b7bdfbec6fb7a8f09dfa5d00b82f0fdae25fb))
* **parser:** take back an account check read past a header's line ([dba3706](https://github.com/robinvdvleuten/beancount/commit/dba37069c42a416ad4897c1ea11094de30c2ef21))
* **parser:** validate an account name at its start only, as beancount does ([8ada3c1](https://github.com/robinvdvleuten/beancount/commit/8ada3c135c03b1534c52051c06e53b6226a93fdf)), closes [#687](https://github.com/robinvdvleuten/beancount/issues/687)
* **parser:** withdraw an open's invalid booking method when the open is dropped ([536275e](https://github.com/robinvdvleuten/beancount/commit/536275e911dd04f24cd6c1c77880cafe4657cdd3))
* **query:** follow each load and ledger error on stderr with a blank line ([92bf7bb](https://github.com/robinvdvleuten/beancount/commit/92bf7bbe4ae9826909ccb2e5f525c709bc632b7e)), closes [#678](https://github.com/robinvdvleuten/beancount/issues/678)
* **query:** print a price without a number as beancount does, "@  USD" ([c5b64d3](https://github.com/robinvdvleuten/beancount/commit/c5b64d3b11debb25f9d566a1aa026c97028705a3)), closes [#683](https://github.com/robinvdvleuten/beancount/issues/683)
* report a load error without context, list includes in load order ([e41a82d](https://github.com/robinvdvleuten/beancount/commit/e41a82dabbdc6811d54d8c5426946b22c19de8af)), closes [#669](https://github.com/robinvdvleuten/beancount/issues/669)
* **web:** keep the editable and watched files in step with the ledger ([4e7e24d](https://github.com/robinvdvleuten/beancount/commit/4e7e24d6ad09f854af2078888f4272cb059e4682)), closes [#671](https://github.com/robinvdvleuten/beancount/issues/671)


### Code Refactoring

* **formatter:** delete the options and helpers nothing uses ([b514d15](https://github.com/robinvdvleuten/beancount/commit/b514d15f938e1918d42bec3c6f953b3c4f9b36c4))
* **formatter:** fail on an item that does not own its line ([31e30ba](https://github.com/robinvdvleuten/beancount/commit/31e30ba896c5b82f0bd3e34ca566ff758cd0385a)), closes [#644](https://github.com/robinvdvleuten/beancount/issues/644)
* give every positioned error of a loaded ledger one shape ([4110ce9](https://github.com/robinvdvleuten/beancount/commit/4110ce906005e2598a7b2288c53d8e2264e9b0f4)), closes [#643](https://github.com/robinvdvleuten/beancount/issues/643)
* **ledger:** unexport the names no other package uses ([c745ea2](https://github.com/robinvdvleuten/beancount/commit/c745ea2af0a8ee4cc99e9d80842a0c3b05682ff6)), closes [#647](https://github.com/robinvdvleuten/beancount/issues/647)

## [0.16.0](https://github.com/robinvdvleuten/beancount/compare/v0.15.0...v0.16.0) (2026-10-03)


### Features

* **query:** read beanquery's accounts Table ([ec58408](https://github.com/robinvdvleuten/beancount/commit/ec58408a3a394eff31d837ff39ced4704acdc6ce)), closes [#639](https://github.com/robinvdvleuten/beancount/issues/639)
* **query:** read beanquery's balances Table ([bfab778](https://github.com/robinvdvleuten/beancount/commit/bfab778c7c05c1c81ae13a5446d23c41b9fe3bb2)), closes [#638](https://github.com/robinvdvleuten/beancount/issues/638)
* **query:** read beanquery's commodities Table ([98b3edf](https://github.com/robinvdvleuten/beancount/commit/98b3edfdf695403a5574ddcb205df83df4df6399)), closes [#635](https://github.com/robinvdvleuten/beancount/issues/635)
* **query:** read beanquery's documents Table ([2b9d6d4](https://github.com/robinvdvleuten/beancount/commit/2b9d6d4afdb86114d68a0c9631cdc0e9752a246f)), closes [#637](https://github.com/robinvdvleuten/beancount/issues/637)
* **query:** read beanquery's events Table ([ed2c657](https://github.com/robinvdvleuten/beancount/commit/ed2c657b9040b68f3135ef5aa226b42b40fb3a3b)), closes [#634](https://github.com/robinvdvleuten/beancount/issues/634)
* **query:** read beanquery's notes Table ([bef50d4](https://github.com/robinvdvleuten/beancount/commit/bef50d4c48a6042adcb142b8b4df087ec998b1a7)), closes [#636](https://github.com/robinvdvleuten/beancount/issues/636)
* **query:** read beanquery's prices Table ([2164a24](https://github.com/robinvdvleuten/beancount/commit/2164a2442f1ba29853481b4eb673b2a7a4fc9458)), closes [#633](https://github.com/robinvdvleuten/beancount/issues/633)
* **query:** read beanquery's transactions Table ([39f1d69](https://github.com/robinvdvleuten/beancount/commit/39f1d69cbb4b34c5f96587436ccd45dedb48cd04)), closes [#632](https://github.com/robinvdvleuten/beancount/issues/632)
* **query:** read the Table SELECT's FROM names ([1e18c81](https://github.com/robinvdvleuten/beancount/commit/1e18c810637adadf204badb05f9c3d000b54b591)), closes [#630](https://github.com/robinvdvleuten/beancount/issues/630) [#618](https://github.com/robinvdvleuten/beancount/issues/618)
* **web:** show the ledger's errors on every page ([3511d51](https://github.com/robinvdvleuten/beancount/commit/3511d51dff489841471d8a3b74fcc2921d8351b9)), closes [#538](https://github.com/robinvdvleuten/beancount/issues/538)


### Bug Fixes

* **parser:** report an invalid account name a syntax error's recovery skips ([982f9f8](https://github.com/robinvdvleuten/beancount/commit/982f9f883021a84dfa7e6052074bc1c21d1c8bc0)), closes [#629](https://github.com/robinvdvleuten/beancount/issues/629)
* **query:** cast false to Python's exponent-0 decimal zero ([2e936a5](https://github.com/robinvdvleuten/beancount/commit/2e936a560c40c04b7c9b626eb2c12b81362b7bbd)), closes [#628](https://github.com/robinvdvleuten/beancount/issues/628)
* **query:** order and group opens and closes by their printed form ([9ad434e](https://github.com/robinvdvleuten/beancount/commit/9ad434eae0bcc4bfe30b9e29b317963eb12b4a0c)), closes [#641](https://github.com/robinvdvleuten/beancount/issues/641)
* **query:** read a note's and a document's tags and links in entries columns ([f862dbb](https://github.com/robinvdvleuten/beancount/commit/f862dbbe8732122fa71c1ba0912275eecf04732a)), closes [#631](https://github.com/robinvdvleuten/beancount/issues/631)

## [0.15.0](https://github.com/robinvdvleuten/beancount/compare/v0.14.0...v0.15.0) (2026-10-02)


### Features

* follow beancount v3 for check, format and doctor ([2f1d757](https://github.com/robinvdvleuten/beancount/commit/2f1d7570e60037cc3be3760124af02405169f32c))
* **parser:** read a capital letter as a currency or a flag ([3b9ebea](https://github.com/robinvdvleuten/beancount/commit/3b9ebea3546d77cc9564f2fe68e91dea5fcc33e9)), closes [#565](https://github.com/robinvdvleuten/beancount/issues/565)
* print directives like beancount v3's printer.py ([13970fb](https://github.com/robinvdvleuten/beancount/commit/13970fbd3e888344adc2a2d3f449fb3ebf894dbd)), closes [#560](https://github.com/robinvdvleuten/beancount/issues/560)
* **query:** add beanquery 0.2's missing BQL functions ([54ad719](https://github.com/robinvdvleuten/beancount/commit/54ad719a7324858d57a798511bca985145d45ca3)), closes [#593](https://github.com/robinvdvleuten/beancount/issues/593)
* **query:** add beanquery's meta, entry and accounts columns ([96a456d](https://github.com/robinvdvleuten/beancount/commit/96a456dcce83ba2c2e1664c81dd8db2658da014b))
* **query:** evaluate BQL functions like beanquery ([5e29dd1](https://github.com/robinvdvleuten/beancount/commit/5e29dd12766915a9c1385025c67a909e085aa135)), closes [#577](https://github.com/robinvdvleuten/beancount/issues/577)
* **query:** give each ORDER BY term its own direction ([b67467a](https://github.com/robinvdvleuten/beancount/commit/b67467a438bcceafd48438a7ccf3f18d3e16ca00)), closes [#575](https://github.com/robinvdvleuten/beancount/issues/575)
* **query:** parse attribute and subscript access on columns ([cf00e3c](https://github.com/robinvdvleuten/beancount/commit/cf00e3c4bfd66582305cc78b10e175404ee03936)), closes [#591](https://github.com/robinvdvleuten/beancount/issues/591)
* **query:** parse count(*), unary minus and digits in names ([fd0b8cc](https://github.com/robinvdvleuten/beancount/commit/fd0b8ccaea7b5e6ed76cc14231f3d95ed9c3ef04)), closes [#574](https://github.com/robinvdvleuten/beancount/issues/574)
* **query:** parse list constants like (1, 2) ([6e06024](https://github.com/robinvdvleuten/beancount/commit/6e06024cc3a494b96dc41c38bf0840cf90e6449d)), closes [#590](https://github.com/robinvdvleuten/beancount/issues/590)
* **query:** print errors like the beanquery shell ([7b4db19](https://github.com/robinvdvleuten/beancount/commit/7b4db19c576f0d7ce0514738829bb11d4f1999bd)), closes [#578](https://github.com/robinvdvleuten/beancount/issues/578)
* **query:** render BQL results like beanquery 0.2.0 ([b69db95](https://github.com/robinvdvleuten/beancount/commit/b69db95a8adb5c3c389545354fc71e7461d55583))
* **query:** support HAVING and PIVOT BY ([0bab56b](https://github.com/robinvdvleuten/beancount/commit/0bab56b396c190cab6d5cf1f583ca08328704c07)), closes [#576](https://github.com/robinvdvleuten/beancount/issues/576)
* **query:** support the %, !~, ?~, NOT IN and BETWEEN operators ([4ca0041](https://github.com/robinvdvleuten/beancount/commit/4ca0041abfeb4a6d86942f2becf420d0cb3439a9)), closes [#587](https://github.com/robinvdvleuten/beancount/issues/587)
* **query:** type-check BQL operators like beanquery ([bb0b3ec](https://github.com/robinvdvleuten/beancount/commit/bb0b3ec02eca01c1257c8da33c2b91bfcae2cf6f)), closes [#573](https://github.com/robinvdvleuten/beancount/issues/573)
* **web:** add a query page that runs BQL ([2e2b03d](https://github.com/robinvdvleuten/beancount/commit/2e2b03d29d84d900af74ab651f428d35d4aff758)), closes [#546](https://github.com/robinvdvleuten/beancount/issues/546)
* **web:** add a trial balance page ([7b5feee](https://github.com/robinvdvleuten/beancount/commit/7b5feee0290843b51e6180d92354931446aa770f)), closes [#544](https://github.com/robinvdvleuten/beancount/issues/544)
* **web:** choose the period of the income statement and balance sheet ([de17183](https://github.com/robinvdvleuten/beancount/commit/de17183ca63817e6d5febb926f61921bbb0c2302)), closes [#545](https://github.com/robinvdvleuten/beancount/issues/545)
* **web:** pick the reports' Valuation ([b7c751d](https://github.com/robinvdvleuten/beancount/commit/b7c751d23205121c9a22cbc04d678c720753e60f)), closes [#536](https://github.com/robinvdvleuten/beancount/issues/536)
* **web:** show the ledger's title in the header and browser tab ([be97a58](https://github.com/robinvdvleuten/beancount/commit/be97a5858ce91865b7e8ec5e556478fb96a4f09b)), closes [#540](https://github.com/robinvdvleuten/beancount/issues/540)


### Bug Fixes

* **ast:** put pushed metadata before a transaction's own ([de2f5fb](https://github.com/robinvdvleuten/beancount/commit/de2f5fb7d4a62fccdc71837729eaea4e1fa0b587))
* **cli:** print an error's transaction with its postings as booked ([bdf094f](https://github.com/robinvdvleuten/beancount/commit/bdf094f8c46c74a5877a05dd2402bc25aa21e18f)), closes [#598](https://github.com/robinvdvleuten/beancount/issues/598)
* **cli:** read only the interpreter from bean-query's shebang ([4de3f0d](https://github.com/robinvdvleuten/beancount/commit/4de3f0db4a112049548ca467d20e71989f54fa57))
* **config:** check options as bean-check does ([8cc0dfe](https://github.com/robinvdvleuten/beancount/commit/8cc0dfe130f4c62a7bf7b2316ff89364e8f814bf)), closes [#568](https://github.com/robinvdvleuten/beancount/issues/568)
* **ledger:** hold no lot of zero units ([0199c2d](https://github.com/robinvdvleuten/beancount/commit/0199c2d901f7f1c7338e1a0bb06839cd3447602c))
* **ledger:** import plugin "beancount.plugins.__init__" as bean-check does ([1f063f8](https://github.com/robinvdvleuten/beancount/commit/1f063f8372a6e47ec5dbd4ca7639642947f260c6)), closes [#569](https://github.com/robinvdvleuten/beancount/issues/569)
* **ledger:** leave a lot undated when its units are interpolated ([232f40a](https://github.com/robinvdvleuten/beancount/commit/232f40a12693d74ae695907b0d888828126129c7)), closes [#597](https://github.com/robinvdvleuten/beancount/issues/597)
* **ledger:** link intermediate accounts with one descendant in the balance tree ([7ae207e](https://github.com/robinvdvleuten/beancount/commit/7ae207e5c8616881573481c3d96f6a4d921a807c))
* **ledger:** word the currency-constraint error as bean-check does ([7704669](https://github.com/robinvdvleuten/beancount/commit/770466949c05f350e7bbdcc6279a7a1bfbb1af6b)), closes [#529](https://github.com/robinvdvleuten/beancount/issues/529)
* **loader:** discover documents under a symlinked documents root ([398c538](https://github.com/robinvdvleuten/beancount/commit/398c5389335bf900ca119a807915f6033e20901f)), closes [#570](https://github.com/robinvdvleuten/beancount/issues/570)
* **parser:** accept total braces without an amount ([ae71825](https://github.com/robinvdvleuten/beancount/commit/ae718252a81609626124ceca6baedab467ed3cb4))
* **parser:** end an empty pushmeta value at its own line ([5e86fca](https://github.com/robinvdvleuten/beancount/commit/5e86fca8dfbd772c7730a72ef44a0d5edca81cb7))
* **parser:** lex a long run of signs in linear time ([9db152b](https://github.com/robinvdvleuten/beancount/commit/9db152b498fb8127e6250faddebf65ac3fbd53b2)), closes [#571](https://github.com/robinvdvleuten/beancount/issues/571)
* **parser:** lex NULL, TRUE and FALSE as keywords, never a currency ([1fd5619](https://github.com/robinvdvleuten/beancount/commit/1fd56195c020c099e5080955a32c9ed3b9f2c1be))
* **parser:** read a merge marker as one cost component among others ([97cb701](https://github.com/robinvdvleuten/beancount/commit/97cb701b721caa175400e5c8f4ce387a85c8b6c4)), closes [#563](https://github.com/robinvdvleuten/beancount/issues/563)
* **parser:** read a NULL metadata value as None ([7f36f8d](https://github.com/robinvdvleuten/beancount/commit/7f36f8d398a755eadf59f853ce4812303b405a11)), closes [#596](https://github.com/robinvdvleuten/beancount/issues/596)
* **parser:** recover from an invalid line and an invalid account as beancount does ([74e5060](https://github.com/robinvdvleuten/beancount/commit/74e5060f1e472ed28547b295f2173b16706a09f8)), closes [#567](https://github.com/robinvdvleuten/beancount/issues/567)
* **parser:** report a duplicate cost component and keep the transaction ([fcffcd0](https://github.com/robinvdvleuten/beancount/commit/fcffcd06b5cee183e34ad734d9c48b170ef7372b))
* **parser:** report every invalid token a syntax error's recovery skips ([a52081a](https://github.com/robinvdvleuten/beancount/commit/a52081ac93f97d436a6ef7027dde0d524ef06bb3))
* **parser:** split a currency off the word it starts, and reject one in a custom ([b7ea123](https://github.com/robinvdvleuten/beancount/commit/b7ea1233975de5458b4a62236dbf719c2a24e401)), closes [#566](https://github.com/robinvdvleuten/beancount/issues/566)
* **parser:** take indented comments among a directive's metadata lines ([9e78024](https://github.com/robinvdvleuten/beancount/commit/9e780240a058b76610b3b72586ff8ae3f2e7fb0d)), closes [#564](https://github.com/robinvdvleuten/beancount/issues/564)
* **query:** accept a LIMIT beyond int64 as beanquery's parser does ([e1e6d45](https://github.com/robinvdvleuten/beancount/commit/e1e6d45ef8dbfd50278f8acf24878f435efbd8f6))
* **query:** allow ORDER BY an aggregate without GROUP BY ([406ba8f](https://github.com/robinvdvleuten/beancount/commit/406ba8fb8e1357eb737e240ecc0a1e4658335b05)), closes [#592](https://github.com/robinvdvleuten/beancount/issues/592)
* **query:** anchor findfirst()'s pattern at the start, like re.match ([83caf09](https://github.com/robinvdvleuten/beancount/commit/83caf09d7ffc06d5a97276b10c249317f28fa7be))
* **query:** count date differences exactly past 292 years ([fda659c](https://github.com/robinvdvleuten/beancount/commit/fda659c8fd9695f90ee05b22e895395a25b75e23))
* **query:** evaluate a missing payee as NULL ([69d973b](https://github.com/robinvdvleuten/beancount/commit/69d973bee1555772cb8430bf59a9f183b5042a19)), closes [#588](https://github.com/robinvdvleuten/beancount/issues/588)
* **query:** evaluate a SELECT's FROM over the postings table ([d9aaaa7](https://github.com/robinvdvleuten/beancount/commit/d9aaaa7114fda3208d07daada2659eaa9d0cafa7)), closes [#582](https://github.com/robinvdvleuten/beancount/issues/582)
* **query:** fail a list IN a set as unhashable, as beanquery does ([09b9b08](https://github.com/robinvdvleuten/beancount/commit/09b9b08e21a18c4a5773e18df20841c2e9f22bb9))
* **query:** fail grepn() with a group out of range, as beanquery does ([8686437](https://github.com/robinvdvleuten/beancount/commit/8686437c2f517518a9a18679544a68fd2e59147f))
* **query:** follow Python's regex, integer and cast semantics ([982cf0f](https://github.com/robinvdvleuten/beancount/commit/982cf0f4dc432c64a2ccd184c0868ed0bde16995)), closes [#589](https://github.com/robinvdvleuten/beancount/issues/589)
* **query:** leave out only the row's own posting in other_accounts ([9823341](https://github.com/robinvdvleuten/beancount/commit/98233413aba8ba4967a377b5499e560dc03d9b45)), closes [#580](https://github.com/robinvdvleuten/beancount/issues/580)
* **query:** let a bool argument fit a function's int parameter ([fa1c2b5](https://github.com/robinvdvleuten/beancount/commit/fa1c2b5659ebe7168c1bda72f0a33d63ffb10b5f))
* **query:** lex beanquery's doubled quotes in strings and quoted identifiers ([c12847d](https://github.com/robinvdvleuten/beancount/commit/c12847d260494c5e21376288c64d84f0c2111dd8))
* **query:** lex Python's white space in BQL ([d7feb6b](https://github.com/robinvdvleuten/beancount/commit/d7feb6bc4a18f365708fb60b622bf8f39f5cf59a)), closes [#585](https://github.com/robinvdvleuten/beancount/issues/585)
* **query:** make cost_date NULL for a cost without a date ([3e84528](https://github.com/robinvdvleuten/beancount/commit/3e84528c348ceb359f69e5d2b651a67129034fb8))
* **query:** make date() match beanquery's overloads ([1a59fe2](https://github.com/robinvdvleuten/beancount/commit/1a59fe298372f4b9ab3d2b334224536f8716b996))
* **query:** make other_accounts a sorted list like beanquery's ([25a2f3e](https://github.com/robinvdvleuten/beancount/commit/25a2f3e3a90b9ae931e4ab0febcfeeebcf030713)), closes [#595](https://github.com/robinvdvleuten/beancount/issues/595)
* **query:** order BQL values as beanquery does ([7b44cc0](https://github.com/robinvdvleuten/beancount/commit/7b44cc0f63bd640b1017225567d97274bbd2cae6)), closes [#579](https://github.com/robinvdvleuten/beancount/issues/579)
* **query:** place the caret at the dot of a decimal after LIMIT ([b81c52e](https://github.com/robinvdvleuten/beancount/commit/b81c52eef252ee8f6645a62fd392f08873517deb)), closes [#586](https://github.com/robinvdvleuten/beancount/issues/586)
* **query:** read ; as beanquery's end-of-line comment and run nothing for an empty query ([daf2fc1](https://github.com/robinvdvleuten/beancount/commit/daf2fc148321af03e3638823663526c178e02fec)), closes [#584](https://github.com/robinvdvleuten/beancount/issues/584)
* **query:** read AT, OPEN, CLOSE, CLEAR and ON as names outside their clauses ([0bb8e8b](https://github.com/robinvdvleuten/beancount/commit/0bb8e8b38f22ed2556d8a57303bb36d0d71d1b3e)), closes [#583](https://github.com/robinvdvleuten/beancount/issues/583)
* **query:** read double-quoted names as function names and columns ([19440cb](https://github.com/robinvdvleuten/beancount/commit/19440cb08f254c93b5d78718ae61b6b4698cd553))
* **query:** read NULL as a name where beanquery does ([d723fdc](https://github.com/robinvdvleuten/beancount/commit/d723fdc46c0427600be54aece44bc76a3d12e8be))
* **query:** read subst()'s replacement as Python's re.sub does ([75cf37a](https://github.com/robinvdvleuten/beancount/commit/75cf37aa663321dcede0b35e00e7b3b1315d0ae9))
* **query:** read the account_previous_* and account_current_* options ([55f9945](https://github.com/robinvdvleuten/beancount/commit/55f99453adaaae5dfeae487510cf5835eb3825dc)), closes [#547](https://github.com/robinvdvleuten/beancount/issues/547)
* **query:** reject integer arguments to abs() and neg() ([7fede21](https://github.com/robinvdvleuten/beancount/commit/7fede21803bbaf688daf5ac55a67c07f0f0a5e75))
* **query:** skip /* … */ block comments in BQL ([cec8730](https://github.com/robinvdvleuten/beancount/commit/cec8730f346904150605c5202796b2c90682a327))
* **query:** take only untyped values in int() and decimal()'s catch-all ([931338e](https://github.com/robinvdvleuten/beancount/commit/931338e61088273bbac371fc7ba1e03abfe5efa6))
* **web:** close Income and Expenses into Equity on the balance sheet ([0683cec](https://github.com/robinvdvleuten/beancount/commit/0683cec00bb04a8e239e43c6f6c95c923a5ce335)), closes [#535](https://github.com/robinvdvleuten/beancount/issues/535)
* **web:** give each operating currency a report column ([45cdf97](https://github.com/robinvdvleuten/beancount/commit/45cdf977ac0dad09cc07a978b83f8d89628cb38d)), closes [#539](https://github.com/robinvdvleuten/beancount/issues/539)
* **web:** keep the current page's sidebar link readable ([e82594e](https://github.com/robinvdvleuten/beancount/commit/e82594eed529b6ac9ab7fdf5e8aa2277532ab2a4))
* **web:** line up the currency columns of the report tables ([1070244](https://github.com/robinvdvleuten/beancount/commit/10702443d40342a5911811f37068c79d0e2ef9c0))
* **web:** load the ledger with syntax recovery ([ab07fcc](https://github.com/robinvdvleuten/beancount/commit/ab07fcc7b4da58ad6196a4b8821994cc7a43fcaa)), closes [#537](https://github.com/robinvdvleuten/beancount/issues/537)
* **web:** reload the balance sheet when the ledger changes ([df22a2a](https://github.com/robinvdvleuten/beancount/commit/df22a2af70f5b02d1e8c3a5124b6d211b197eb61)), closes [#541](https://github.com/robinvdvleuten/beancount/issues/541)
* **web:** show the file-change toast only for reload events ([527d97d](https://github.com/robinvdvleuten/beancount/commit/527d97de5f1f41a6603def7b6b30f09747bd2ba3))


### Performance Improvements

* **ledger:** book an augmentation without cloning its inventory ([d40e7fe](https://github.com/robinvdvleuten/beancount/commit/d40e7fe481bac9ec02f619e1e31c09333f0af27f)), closes [#572](https://github.com/robinvdvleuten/beancount/issues/572)

## [0.14.0](https://github.com/robinvdvleuten/beancount/compare/v0.13.0...v0.14.0) (2026-09-29)


### ⚠ BREAKING CHANGES

* **parser:** ast.Metadata has no Inline field.
* **ledger:** ledger.Graph, Node, Edge, NodeKind, EdgeKind, their constants, NewGraph, Stats, CommodityNode, Ledger.Graph, Account.GetParent and Account.GetChildren are removed, and CommodityDelta keeps only CommodityID.
* **ledger:** ledger.InferTolerance, ledger.ToleranceConfig and ledger.NewToleranceConfig are removed. Tolerance options live in config.Tolerance (config.NewTolerance).
* **ledger:** ledger.Account and ledger.OpenDelta no longer have a BookingMethod field.
* **query:** the query package exports only Run, Context (with the new AST field), Format, FormatText and FormatCSV. Removed from the API: Compile, CompilePrint, Execute, ExecutePrint, RenderText, RenderCSV, Desugar, NewInventory, NewSet, and the types Compiled, CompiledFrom, CompiledTarget, CompileError, CompiledPrint, Result, ResultColumn, Row, Amount, Cost, Position, Inventory, Set and DType with its TAny ... TInventory constants. Run PRINT and every other statement through query.Run.
* **query:** formatter.WithParsedNumbers and Formatter.FormatTransaction are removed; use printer.Sprint or printer.Print to render directives without a source.
* **ledger:** Inventory.Book and Inventory.Add are removed from the public API; booking a posting is internal to the ledger package.
* **ledger:** Ledger.BookedLots and BookedLot are replaced by Ledger.BookedPositions, BookedPosition and BookedCost; PerUnitCost is no longer exported.

### Features

* **ledger:** publish booked positions per posting ([1ca0829](https://github.com/robinvdvleuten/beancount/commit/1ca0829d3c89693c9cb98d3f7dd2adfc155a8dc9)), closes [#482](https://github.com/robinvdvleuten/beancount/issues/482)
* **query:** print directives through a printer like beancount's ([819b941](https://github.com/robinvdvleuten/beancount/commit/819b941d6280c9a0144ecaf378856f6cd53bfd9d)), closes [#485](https://github.com/robinvdvleuten/beancount/issues/485)
* **query:** run BQL through one Run entry, with PRINT in the pipeline ([fe4a6bc](https://github.com/robinvdvleuten/beancount/commit/fe4a6bcb919010e9f921539c1ad0fa63b855b46f)), closes [#486](https://github.com/robinvdvleuten/beancount/issues/486)


### Bug Fixes

* **cli:** collect import's stderr in a concurrency-safe buffer ([f5a81e9](https://github.com/robinvdvleuten/beancount/commit/f5a81e9fe25404adb0865829730696ad02a12cba))
* **cli:** show a positioned error in the context of its own file ([4a6f62c](https://github.com/robinvdvleuten/beancount/commit/4a6f62ce4803bdd494951c583f24afecddbf1755)), closes [#492](https://github.com/robinvdvleuten/beancount/issues/492)
* generate valid dates in the large-file generator ([01c62b5](https://github.com/robinvdvleuten/beancount/commit/01c62b54e03c33d33ce88fa23107100e67136433))
* **ledger:** check a balance's currency against its account's open, wherever dated ([56c5b21](https://github.com/robinvdvleuten/beancount/commit/56c5b217cda3323be41177f39ec942c4d0a5f1d1)), closes [#525](https://github.com/robinvdvleuten/beancount/issues/525)
* **ledger:** drop a zero-unit posting at a cost to infer, like beancount ([dacc453](https://github.com/robinvdvleuten/beancount/commit/dacc45353f574f7cda6ff8a0e8f824946ea80cbe))
* **ledger:** end the balance currency error with bean-check's ": " ([a4a5f5b](https://github.com/robinvdvleuten/beancount/commit/a4a5f5b8fb7b5b9bcda71601d2a0d8c88f8d7fbc)), closes [#526](https://github.com/robinvdvleuten/beancount/issues/526)
* **ledger:** interpolate a compound cost's missing number, as {# 5 USD} ([3c4e9b2](https://github.com/robinvdvleuten/beancount/commit/3c4e9b22b2e26d13e0ac49d27ed2bade408a0570)), closes [#528](https://github.com/robinvdvleuten/beancount/issues/528)
* **ledger:** keep the exponent of an interpolated cost ([1d31efc](https://github.com/robinvdvleuten/beancount/commit/1d31efcc9748b74f727fa0835364af403f5eb2a0)), closes [#511](https://github.com/robinvdvleuten/beancount/issues/511)
* **ledger:** keep the written scale of implicit prices ([fca110f](https://github.com/robinvdvleuten/beancount/commit/fca110f2a46991d36326a52e5722af3e295a83bc))
* **ledger:** pad and check balances on accounts outside their interval ([2c7b586](https://github.com/robinvdvleuten/beancount/commit/2c7b5863a43fbef96b22997b64c8a0c14f257db2)), closes [#517](https://github.com/robinvdvleuten/beancount/issues/517)
* **ledger:** print currency group errors without an account suffix ([282bf68](https://github.com/robinvdvleuten/beancount/commit/282bf6881ac692a5f28e7dede3d843fa9919610f)), closes [#527](https://github.com/robinvdvleuten/beancount/issues/527)
* **ledger:** report a pad that fills a currency held at cost ([68c645b](https://github.com/robinvdvleuten/beancount/commit/68c645bc18ae8f75de885c51d68905ef382b1160)), closes [#509](https://github.com/robinvdvleuten/beancount/issues/509)
* **ledger:** report a reference outside an opened account's interval as inactive ([8101865](https://github.com/robinvdvleuten/beancount/commit/8101865f36fc69db3949c716b891d36a823db999)), closes [#516](https://github.com/robinvdvleuten/beancount/issues/516)
* **ledger:** report a transaction added after Booking, not skip it ([abadc32](https://github.com/robinvdvleuten/beancount/commit/abadc32b2e73afd1b6bd699a500425dcec32b469)), closes [#491](https://github.com/robinvdvleuten/beancount/issues/491)
* **ledger:** report zero units at cost, like bean-check ([178187f](https://github.com/robinvdvleuten/beancount/commit/178187f70ec75edc27d90a5c66bf7e51c9c6aa4f))
* **ledger:** word booking errors as bean-check does ([39b70d9](https://github.com/robinvdvleuten/beancount/commit/39b70d9cadb667c66d5854deadd508eb86940193)), closes [#518](https://github.com/robinvdvleuten/beancount/issues/518)
* **ledger:** word the merge cost error as bean-check does ([91dfbeb](https://github.com/robinvdvleuten/beancount/commit/91dfbeb2034fd347ad765a713ae37530844aaff1)), closes [#523](https://github.com/robinvdvleuten/beancount/issues/523)
* **loader:** ignore plugin directives in included files ([9bda881](https://github.com/robinvdvleuten/beancount/commit/9bda8810293548262e88b56b17a626e815ca65e9)), closes [#508](https://github.com/robinvdvleuten/beancount/issues/508)
* **parser:** accept a cost number without a currency, as {10} ([152e66e](https://github.com/robinvdvleuten/beancount/commit/152e66eb3089db3876e1bac3d414092f48efb9b5)), closes [#522](https://github.com/robinvdvleuten/beancount/issues/522)
* **parser:** end a dated directive's header at its line ([c113bdf](https://github.com/robinvdvleuten/beancount/commit/c113bdfb6dc9e0fdbecb4d8fd2d2e46ab2496bec)), closes [#507](https://github.com/robinvdvleuten/beancount/issues/507)
* **parser:** end a header on the line its string spanning lines closes ([a6aaf49](https://github.com/robinvdvleuten/beancount/commit/a6aaf499534b0d9f306dd51acce03bafc6f8f299)), closes [#521](https://github.com/robinvdvleuten/beancount/issues/521)
* **parser:** reject inline metadata and undated lines split over two ([84072b9](https://github.com/robinvdvleuten/beancount/commit/84072b940047a29acee75851fb21accb7095e39b)), closes [#519](https://github.com/robinvdvleuten/beancount/issues/519)
* **parser:** skip column-1 flag lines like beancount's lexer ([82f2cba](https://github.com/robinvdvleuten/beancount/commit/82f2cba72f614e95fff145d5571496bd075bc5f0)), closes [#495](https://github.com/robinvdvleuten/beancount/issues/495)
* **query:** add beancount's conversion entry to FROM OPEN and CLOSE ([d2ed634](https://github.com/robinvdvleuten/beancount/commit/d2ed63447e8af16e1b6f92b30776a28e728a04cc))
* **query:** compare values of different types as never equal ([0dfd649](https://github.com/robinvdvleuten/beancount/commit/0dfd6498809a423a0d7ccde0878285a212d33e61)), closes [#514](https://github.com/robinvdvleuten/beancount/issues/514)
* **query:** evaluate balance in WHERE as the running balance so far ([dcc0336](https://github.com/robinvdvleuten/beancount/commit/dcc0336f841d8053d1bc9ec50008da2469373a6c)), closes [#520](https://github.com/robinvdvleuten/beancount/issues/520)
* **query:** fold earnings in sorted account order, like bean-query ([3300dd8](https://github.com/robinvdvleuten/beancount/commit/3300dd83c9b3026155900264ca6331f7f8a59125))
* **query:** keep only active opens and last prices before FROM OPEN ([c847a3f](https://github.com/robinvdvleuten/beancount/commit/c847a3f5cf48ee6478a603e164bd0a7f8c2807f0))
* **query:** print a number below 1E-6 in Python's exponent form ([bc10de4](https://github.com/robinvdvleuten/beancount/commit/bc10de4b6837f3eb862129a1502f52ecf24c90a9)), closes [#512](https://github.com/robinvdvleuten/beancount/issues/512)
* **query:** print open and price lines in beancount's printer columns ([75d17d0](https://github.com/robinvdvleuten/beancount/commit/75d17d0da1a32d10b9a9ec2eb3951def11647f81))
* **query:** reject a FROM clause whose CLOSE ON precedes its OPEN ON ([27d9a0d](https://github.com/robinvdvleuten/beancount/commit/27d9a0dfdbad5bbfee04e221238671dcbdd05d9f)), closes [#513](https://github.com/robinvdvleuten/beancount/issues/513)
* **query:** skip a NULL position in sum ([e0cf77a](https://github.com/robinvdvleuten/beancount/commit/e0cf77a3934604766deac591359ac45b124d5a58))


### Performance Improvements

* **ledger:** find the lot a posting adds to by key ([bde8414](https://github.com/robinvdvleuten/beancount/commit/bde84141de023e487ede879277da097705ceb59d)), closes [#497](https://github.com/robinvdvleuten/beancount/issues/497)
* **parser:** find error context lines in linear time ([0c5a66f](https://github.com/robinvdvleuten/beancount/commit/0c5a66fdec35ff4e6e835152c47cdc50e18d923e))


### Code Refactoring

* **ledger:** decide and book augment vs reduce on the inventory ([cab569f](https://github.com/robinvdvleuten/beancount/commit/cab569f4ce1b1a5e442688648ee44c483b2b54a2)), closes [#483](https://github.com/robinvdvleuten/beancount/issues/483)
* **ledger:** delete dead Account.BookingMethod ([092fd9a](https://github.com/robinvdvleuten/beancount/commit/092fd9aa51a9021abe3b9de5fee1e4ccafc320a4)), closes [#493](https://github.com/robinvdvleuten/beancount/issues/493)
* **ledger:** move tolerance rules off the booker into one module ([7e187fb](https://github.com/robinvdvleuten/beancount/commit/7e187fb9e8c50c45404dfca2f040c74aea030149)), closes [#489](https://github.com/robinvdvleuten/beancount/issues/489)
* **ledger:** replace Graph with a price index and plain balance tree ([7c4f73b](https://github.com/robinvdvleuten/beancount/commit/7c4f73bb3a04230d0256b86b7ac9c25226d850bb)), closes [#487](https://github.com/robinvdvleuten/beancount/issues/487)

## [0.13.0](https://github.com/robinvdvleuten/beancount/compare/v0.12.0...v0.13.0) (2026-09-27)


### Features

* add beancount import and the importer SDK ([#454](https://github.com/robinvdvleuten/beancount/issues/454)) ([eb150a0](https://github.com/robinvdvleuten/beancount/commit/eb150a0d95404cd2b8d52159e2d3a2a18b2bc319)), closes [#437](https://github.com/robinvdvleuten/beancount/issues/437)
* **cli:** add doctor missing_open like bean-doctor ([10a3fa4](https://github.com/robinvdvleuten/beancount/commit/10a3fa4c9b87a93a1eb6679271d164ffbb12651b)), closes [#481](https://github.com/robinvdvleuten/beancount/issues/481)
* **cli:** drop Duplicates on import and add --unknown-account ([be00d18](https://github.com/robinvdvleuten/beancount/commit/be00d18451cd67382a9b8debaf429c1b33ba0a6a)), closes [#438](https://github.com/robinvdvleuten/beancount/issues/438)
* **ledger:** run Built-in Plugins auto_accounts and implicit_prices ([#447](https://github.com/robinvdvleuten/beancount/issues/447)) ([d8e0b56](https://github.com/robinvdvleuten/beancount/commit/d8e0b5614fdc16bfad3737eb140918095ce2e92a)), closes [#436](https://github.com/robinvdvleuten/beancount/issues/436)


### Bug Fixes

* **cli:** report errors on bean-check's lines ([#448](https://github.com/robinvdvleuten/beancount/issues/448)) ([848b003](https://github.com/robinvdvleuten/beancount/commit/848b0035fd5c0849b9791c9de000285c0969cf52)), closes [#442](https://github.com/robinvdvleuten/beancount/issues/442)
* divide to Python's 28 significant digits ([447ec16](https://github.com/robinvdvleuten/beancount/commit/447ec1665f2be893c897d9198c280cf97cd844b3)), closes [#392](https://github.com/robinvdvleuten/beancount/issues/392)
* **formatter:** copy an aligned posting line from its currency on ([e725098](https://github.com/robinvdvleuten/beancount/commit/e7250980cf2e0b7f6011ebc60d17722d4c139c19)), closes [#458](https://github.com/robinvdvleuten/beancount/issues/458)
* **importer:** keep Importer stderr output when the host closes it ([3cafcbc](https://github.com/robinvdvleuten/beancount/commit/3cafcbc9a4cb31a4cdb50e328a591db78a509eec))
* **ledger:** accept a zero price directive, like bean-check ([e68b27b](https://github.com/robinvdvleuten/beancount/commit/e68b27b80326dab8b99c0aa6d87485e92ffbf516)), closes [#472](https://github.com/robinvdvleuten/beancount/issues/472)
* **ledger:** accept empty metadata values, like bean-check ([bc33073](https://github.com/robinvdvleuten/beancount/commit/bc330732b6d81dbb217a5bb23de0acb1292f9bbd))
* **ledger:** accept empty note descriptions and document paths, like bean-check ([40b163b](https://github.com/robinvdvleuten/beancount/commit/40b163bdfba61fd756fbf60c4bc3c7448943aa0e))
* **ledger:** apply a pad's padding at the balance assertion it fills ([a95b717](https://github.com/robinvdvleuten/beancount/commit/a95b7170f0eaeedca64aa4b10e6b68b6e0ece348)), closes [#462](https://github.com/robinvdvleuten/beancount/issues/462)
* **ledger:** apply directives that fail validation, like beancount ([a7f7aba](https://github.com/robinvdvleuten/beancount/commit/a7f7abac3b4e2fcbd95ff09a7e862b545c3ab685)), closes [#410](https://github.com/robinvdvleuten/beancount/issues/410)
* **ledger:** book a transaction's lots only through the groups it books ([271c04e](https://github.com/robinvdvleuten/beancount/commit/271c04e025bb6d6869365ba8a4579dd18c463549)), closes [#463](https://github.com/robinvdvleuten/beancount/issues/463)
* **ledger:** book each currency group on its own, like beancount v2 ([#451](https://github.com/robinvdvleuten/beancount/issues/451)) ([7cd1611](https://github.com/robinvdvleuten/beancount/commit/7cd16118b8c5107cbbab9acf566b0b799725a882)), closes [#445](https://github.com/robinvdvleuten/beancount/issues/445)
* **ledger:** give a posting's per-unit price one owner, like beancount ([22acd16](https://github.com/robinvdvleuten/beancount/commit/22acd165ab372c7677b1ceda5c3e406dfac4b0b1)), closes [#465](https://github.com/robinvdvleuten/beancount/issues/465)
* **ledger:** keep a padding's own precision, like beancount ([282a308](https://github.com/robinvdvleuten/beancount/commit/282a3082d5c8b0efa95e7150c7c1997dd8a75f19)), closes [#468](https://github.com/robinvdvleuten/beancount/issues/468)
* **ledger:** list the lots a failed booking saw, not later ones ([dded349](https://github.com/robinvdvleuten/beancount/commit/dded3492f68c3d5b1805cc60b7233991908fa713)), closes [#461](https://github.com/robinvdvleuten/beancount/issues/461)
* **ledger:** report a balance in a currency its account does not allow ([ffd8bec](https://github.com/robinvdvleuten/beancount/commit/ffd8becc7954e08f24c0d10a0b0b695084207a3d)), closes [#443](https://github.com/robinvdvleuten/beancount/issues/443)
* **ledger:** report duplicate balance assertions with different amounts ([54be97e](https://github.com/robinvdvleuten/beancount/commit/54be97e78591576807151c1b5fe2c7c3f9761930)), closes [#460](https://github.com/robinvdvleuten/beancount/issues/460)
* **ledger:** report negative prices and total prices without units ([cce0855](https://github.com/robinvdvleuten/beancount/commit/cce0855403ef6763cb69136fde4e7e0a7f7203b5)), closes [#478](https://github.com/robinvdvleuten/beancount/issues/478)
* **ledger:** report plugins under beancount.plugins that v2 doesn't ship ([7b05d2e](https://github.com/robinvdvleuten/beancount/commit/7b05d2eca4929da72effa17232c55e4cdc8faaf8)), closes [#479](https://github.com/robinvdvleuten/beancount/issues/479)
* **ledger:** use a pad even when its balance assertion fails ([#453](https://github.com/robinvdvleuten/beancount/issues/453)) ([2301c6c](https://github.com/robinvdvleuten/beancount/commit/2301c6cd4c894520917d2f9897099ad92d63d59b)), closes [#446](https://github.com/robinvdvleuten/beancount/issues/446)
* **loader:** report a missing include as an unmatched glob, like bean-check ([018263b](https://github.com/robinvdvleuten/beancount/commit/018263bb430f18ecc4bcffc4aed8fb8b3c25bd2d))
* **parser:** accept a cost that gives only its currency, like beancount ([#459](https://github.com/robinvdvleuten/beancount/issues/459)) ([b15009e](https://github.com/robinvdvleuten/beancount/commit/b15009e2a031aaae3b3313ff02c98edfaf9082d7)), closes [#449](https://github.com/robinvdvleuten/beancount/issues/449)
* **parser:** recover from syntax errors, like beancount ([#455](https://github.com/robinvdvleuten/beancount/issues/455)) ([deb99c1](https://github.com/robinvdvleuten/beancount/commit/deb99c17805f62979e574a3af03319d55aea6fd0)), closes [#446](https://github.com/robinvdvleuten/beancount/issues/446)
* **parser:** reject a lowercase currency, like bean-check ([9d4a563](https://github.com/robinvdvleuten/beancount/commit/9d4a563233c98a4d5dc7ffc43fc59de285850749)), closes [#456](https://github.com/robinvdvleuten/beancount/issues/456)
* **query:** convert through a currency pair's own price, like bean-query ([2641e5f](https://github.com/robinvdvleuten/beancount/commit/2641e5f3980e514470f05b1e54e6aff7745df8d9)), closes [#477](https://github.com/robinvdvleuten/beancount/issues/477)
* **query:** give repeated column names bean-query's _1, _2 suffixes ([e20c900](https://github.com/robinvdvleuten/beancount/commit/e20c900c37df671282f38df52a1c5cfea0191c0c)), closes [#476](https://github.com/robinvdvleuten/beancount/issues/476)
* **query:** lay out print like bean-query's print_entries ([baf7465](https://github.com/robinvdvleuten/beancount/commit/baf7465b53049671483c18558e29d5df5f724aeb)), closes [#470](https://github.com/robinvdvleuten/beancount/issues/470)
* **query:** leave the source's comments out of print, like bean-query ([5f4e544](https://github.com/robinvdvleuten/beancount/commit/5f4e544c82e85fe12f97cb8b72c80fdcaa9285d0)), closes [#474](https://github.com/robinvdvleuten/beancount/issues/474)
* **query:** print a failed balance assertion's difference ([07287fa](https://github.com/robinvdvleuten/beancount/commit/07287fa9eb207ebac69b3b4b747e52cf2179b8b3)), closes [#473](https://github.com/robinvdvleuten/beancount/issues/473)
* **query:** print booked postings, like bean-query ([1d0c804](https://github.com/robinvdvleuten/beancount/commit/1d0c80422de79a2874064ed0cd43bc8326688b39)), closes [#450](https://github.com/robinvdvleuten/beancount/issues/450)
* **query:** print numbers as parsed, like bean-query ([4966c07](https://github.com/robinvdvleuten/beancount/commit/4966c07ce17535af36fbc181e6f0179c79011e48)), closes [#469](https://github.com/robinvdvleuten/beancount/issues/469)
* **query:** render str() of positions and inventories like bean-query ([5feeaf3](https://github.com/robinvdvleuten/beancount/commit/5feeaf3ede65df9bb7e69acbe77bf0269fa8705f)), closes [#407](https://github.com/robinvdvleuten/beancount/issues/407)
* round sums and products to Python's 28 significant digits ([dc26f99](https://github.com/robinvdvleuten/beancount/commit/dc26f999c83e9d1bc0c77d1b83f29cd28587e114)), closes [#439](https://github.com/robinvdvleuten/beancount/issues/439)

## [0.12.0](https://github.com/robinvdvleuten/beancount/compare/v0.11.0...v0.12.0) (2026-09-25)


### Features

* **loader:** support glob patterns in include directives ([#356](https://github.com/robinvdvleuten/beancount/issues/356)) ([08fefd4](https://github.com/robinvdvleuten/beancount/commit/08fefd43a9d0588d18b4af0ad0a36e4a06d0b7cc))


### Bug Fixes

* **ast:** apply pushed metadata to transactions only, typed ([86179e5](https://github.com/robinvdvleuten/beancount/commit/86179e55a6a518269a40c7e82fb3a3786a75e4fe)), closes [#413](https://github.com/robinvdvleuten/beancount/issues/413)
* **ast:** report unbalanced pushtag/pushmeta and pops of absent ones ([3f6218f](https://github.com/robinvdvleuten/beancount/commit/3f6218f498bda06c0d8404b4177113fcb2744407)), closes [#374](https://github.com/robinvdvleuten/beancount/issues/374)
* **config:** apply options one by one, like beancount ([485e853](https://github.com/robinvdvleuten/beancount/commit/485e853321281bbe1b8fa77777c35b6769b2fe90)), closes [#412](https://github.com/robinvdvleuten/beancount/issues/412)
* **config:** match the booking_method option case-sensitively ([7a0e707](https://github.com/robinvdvleuten/beancount/commit/7a0e7074ab34dec220816169468abf708758ac43)), closes [#411](https://github.com/robinvdvleuten/beancount/issues/411)
* **formatter:** align amounts where bean-format's line pattern does ([59b8915](https://github.com/robinvdvleuten/beancount/commit/59b89153017d0fc8269d6cac6a410b28a53ab10c)), closes [#386](https://github.com/robinvdvleuten/beancount/issues/386)
* **formatter:** keep bean-format's prefix and number widths apart ([1efe215](https://github.com/robinvdvleuten/beancount/commit/1efe215aba53be618fd8d377bbda69eb170d3fb2)), closes [#426](https://github.com/robinvdvleuten/beancount/issues/426)
* **formatter:** keep comment and whitespace-only lines as written ([6dd1da5](https://github.com/robinvdvleuten/beancount/commit/6dd1da5f8c5bbcf3935d278314918869f7e5167c)), closes [#416](https://github.com/robinvdvleuten/beancount/issues/416)
* **formatter:** keep posting metadata lines as written ([634d92e](https://github.com/robinvdvleuten/beancount/commit/634d92e7fe11589ce8397df0224c486a0e374b0f)), closes [#425](https://github.com/robinvdvleuten/beancount/issues/425)
* **formatter:** keep the source spelling of headers, dates and strings ([ecba440](https://github.com/robinvdvleuten/beancount/commit/ecba4405ce4f7fa340522aed6deadb8e13bbb1c3)), closes [#393](https://github.com/robinvdvleuten/beancount/issues/393)
* **formatter:** leave numbers glued to their currency as written ([e86a78a](https://github.com/robinvdvleuten/beancount/commit/e86a78af08be5fecf2dd6d0fb5e6eec5b3654aa4)), closes [#428](https://github.com/robinvdvleuten/beancount/issues/428)
* **formatter:** leave postings with incomplete amounts as written ([1e9fba1](https://github.com/robinvdvleuten/beancount/commit/1e9fba144264684dd919ef970522483935702cf2)), closes [#385](https://github.com/robinvdvleuten/beancount/issues/385)
* **ledger:** a pad from an account to itself does not satisfy a balance ([2146143](https://github.com/robinvdvleuten/beancount/commit/2146143fe7bef83f548a3ba6c68faed53e9b6923)), closes [#376](https://github.com/robinvdvleuten/beancount/issues/376)
* **ledger:** book an amount-less posting once per residual currency ([7641e38](https://github.com/robinvdvleuten/beancount/commit/7641e380dc023c41d36033bd06ab4173dae8ea34)), closes [#399](https://github.com/robinvdvleuten/beancount/issues/399)
* **ledger:** book cost-spec reductions only against lots held at cost ([142f50d](https://github.com/robinvdvleuten/beancount/commit/142f50d727d4b013deb48dade07044b6c77d810a)), closes [#409](https://github.com/robinvdvleuten/beancount/issues/409)
* **ledger:** book postings in beancount's currency-grouped order ([660b584](https://github.com/robinvdvleuten/beancount/commit/660b584d3cd1a8e2e1b5015e201467e02272e12a)), closes [#400](https://github.com/robinvdvleuten/beancount/issues/400)
* **ledger:** book short positions at cost like beancount ([6d294ab](https://github.com/robinvdvleuten/beancount/commit/6d294ab8bb1c6d7abfa340c31e15dc4a6cfded25)), closes [#378](https://github.com/robinvdvleuten/beancount/issues/378)
* **ledger:** interpolate missing units from a per-unit cost or price ([7a7c55d](https://github.com/robinvdvleuten/beancount/commit/7a7c55d1e13ec9e3af9461564c82c1ac2d36fd62)), closes [#379](https://github.com/robinvdvleuten/beancount/issues/379)
* **ledger:** pad each currency once and only count pads that insert padding ([caf3629](https://github.com/robinvdvleuten/beancount/commit/caf3629dbe4247844da2e224ecab640c604f1acf)), closes [#415](https://github.com/robinvdvleuten/beancount/issues/415)
* **ledger:** print an unused pad's directive only once ([bf2e45f](https://github.com/robinvdvleuten/beancount/commit/bf2e45f3876e52fe03d04edd04a85803b1712a7b)), closes [#391](https://github.com/robinvdvleuten/beancount/issues/391)
* **ledger:** reject duplicate commodity directives ([b34b4f6](https://github.com/robinvdvleuten/beancount/commit/b34b4f60a4f55604e03de23276a566352ae9b274)), closes [#371](https://github.com/robinvdvleuten/beancount/issues/371)
* **ledger:** reject postings booked at a negative cost ([089ed40](https://github.com/robinvdvleuten/beancount/commit/089ed40604159d3a4fa57ae3870768bdc13e2d0a)), closes [#372](https://github.com/robinvdvleuten/beancount/issues/372)
* **ledger:** reject unknown booking methods on open directives ([1cec958](https://github.com/robinvdvleuten/beancount/commit/1cec9581fd50ee257bf7189086d95427104e5c80)), closes [#373](https://github.com/robinvdvleuten/beancount/issues/373)
* **ledger:** report over-reductions as "not enough lots" ([1fe23ce](https://github.com/robinvdvleuten/beancount/commit/1fe23ceec786a9f3112af2c15d2499d36e52ddb5)), closes [#383](https://github.com/robinvdvleuten/beancount/issues/383)
* **ledger:** round interpolated amounts to the transaction's tolerance ([288d688](https://github.com/robinvdvleuten/beancount/commit/288d688dde1336589b6f2787ea0c7cdf460c523a)), closes [#404](https://github.com/robinvdvleuten/beancount/issues/404)
* **ledger:** widen tolerances by cost and price under infer_tolerance_from_cost ([af46ee3](https://github.com/robinvdvleuten/beancount/commit/af46ee3a29785a46f2744313f4a98011f1d839d9)), closes [#406](https://github.com/robinvdvleuten/beancount/issues/406)
* **loader:** report duplicate includes instead of skipping them silently ([87996e0](https://github.com/robinvdvleuten/beancount/commit/87996e01454714e468b5784d9f8c73a4bd1bd82d)), closes [#375](https://github.com/robinvdvleuten/beancount/issues/375)
* **parser:** accept tag and link lines in a transaction body ([d6fbd5b](https://github.com/robinvdvleuten/beancount/commit/d6fbd5b430fd2832437a413b7728a98d6d66986a)), closes [#382](https://github.com/robinvdvleuten/beancount/issues/382)
* **parser:** accept trailing-dot numbers and repeated or spaced signs ([b682730](https://github.com/robinvdvleuten/beancount/commit/b6827301ebd83546fd9751226d15f70932571418)), closes [#381](https://github.com/robinvdvleuten/beancount/issues/381)
* **parser:** keep org-mode lines as comments instead of dropping them ([249f864](https://github.com/robinvdvleuten/beancount/commit/249f8641e2ef9dfff5776eabb13715a140876722)), closes [#384](https://github.com/robinvdvleuten/beancount/issues/384)
* **parser:** keep the precision of arithmetic amounts ([ccf1df6](https://github.com/robinvdvleuten/beancount/commit/ccf1df6d8754aba1c1f40a0b2819a350618527d4)), closes [#405](https://github.com/robinvdvleuten/beancount/issues/405)
* **parser:** read a posting's number only from the posting's line ([30dbb8a](https://github.com/robinvdvleuten/beancount/commit/30dbb8ae41fc9793b3031eb64e93c4f19e0aa4c4)), closes [#427](https://github.com/robinvdvleuten/beancount/issues/427)
* **parser:** reject indented lines at top level ([02de990](https://github.com/robinvdvleuten/beancount/commit/02de9906c2a0f0f4e721e1aa6e9a95aa3ac8aad4)), closes [#377](https://github.com/robinvdvleuten/beancount/issues/377)
* **query:** accept a WHERE clause on the BALANCES shortcut ([af80029](https://github.com/robinvdvleuten/beancount/commit/af800291875a871c073aae4dfa7dedb42bc23b75)), closes [#387](https://github.com/robinvdvleuten/beancount/issues/387)
* **query:** compile each clause in its own environment, like bean-query ([0417bd6](https://github.com/robinvdvleuten/beancount/commit/0417bd6e43fa9490211f5c7d7d0489b116ff3ce9)), closes [#421](https://github.com/robinvdvleuten/beancount/issues/421)
* **query:** keep inventory positions in insertion order ([ff54dec](https://github.com/robinvdvleuten/beancount/commit/ff54dec2c36db1519e77fc5525e877adb57012d6)), closes [#388](https://github.com/robinvdvleuten/beancount/issues/388)
* **query:** lex BQL numbers and identifiers like bean-query ([13e7bf0](https://github.com/robinvdvleuten/beancount/commit/13e7bf067665d2c556bb7eec89f62e7057a013a0)), closes [#420](https://github.com/robinvdvleuten/beancount/issues/420)
* **query:** match bean-query's column names and empty-column widths ([971d45f](https://github.com/robinvdvleuten/beancount/commit/971d45f0699310e9dfd08cca3a62f2a20273fe6b)), closes [#389](https://github.com/robinvdvleuten/beancount/issues/389)
* **query:** match has_account against every account an entry references ([952eb13](https://github.com/robinvdvleuten/beancount/commit/952eb1379f9efa6ead1273fc88f66094ec2e0a90)), closes [#423](https://github.com/robinvdvleuten/beancount/issues/423)
* **query:** parse PIVOT BY as column names and reject it at compile time ([c891b36](https://github.com/robinvdvleuten/beancount/commit/c891b3675c04f2df12ea83ba52c1c4664696d596)), closes [#424](https://github.com/robinvdvleuten/beancount/issues/424)
* **query:** post one equity leg per lot in summarized balances ([d777bea](https://github.com/robinvdvleuten/beancount/commit/d777bea400837528a188cc24458230a8c17d3b31)), closes [#398](https://github.com/robinvdvleuten/beancount/issues/398)
* **query:** print (empty) for empty results in every output format ([a21bc2d](https://github.com/robinvdvleuten/beancount/commit/a21bc2d220a6e90835f9bca7e5f66b5ba282f6fb)), closes [#402](https://github.com/robinvdvleuten/beancount/issues/402)
* **query:** reject function arguments like bean-query's fallback classes ([076074a](https://github.com/robinvdvleuten/beancount/commit/076074afdf133c1622dfbbeca42d613cc806c026)), closes [#419](https://github.com/robinvdvleuten/beancount/issues/419)
* **query:** render metadata values like bean-query's ObjectRenderer ([5f3613f](https://github.com/robinvdvleuten/beancount/commit/5f3613fdcb07516e6d62d6768d2e679c92a89067)), closes [#414](https://github.com/robinvdvleuten/beancount/issues/414)
* **query:** render numbers at the ledger's display precision ([d359e51](https://github.com/robinvdvleuten/beancount/commit/d359e51de67bc582b20b5dcb78481e5c958d1d0e)), closes [#401](https://github.com/robinvdvleuten/beancount/issues/401)
* **query:** render set columns like bean-query ([f13214f](https://github.com/robinvdvleuten/beancount/commit/f13214f54ca7b9e8142d1ad7bc37257feaa18425)), closes [#418](https://github.com/robinvdvleuten/beancount/issues/418)
* **query:** report BQL errors in bean-query's words ([31539a3](https://github.com/robinvdvleuten/beancount/commit/31539a3f3e0d48c01a03d852891f9a52a5c68170)), closes [#390](https://github.com/robinvdvleuten/beancount/issues/390)
* **query:** reserve a sign column when rendering numbers ([06be44f](https://github.com/robinvdvleuten/beancount/commit/06be44f3fbee64f3993eee0de09d96f658cc0b1c)), closes [#397](https://github.com/robinvdvleuten/beancount/issues/397)
* **query:** take a posting's per-unit cost from the ledger ([103648e](https://github.com/robinvdvleuten/beancount/commit/103648ecc2a2c45958acccfd3784e624d03c1d2d)), closes [#417](https://github.com/robinvdvleuten/beancount/issues/417)
* **query:** take booked lots from the ledger instead of re-deriving them ([e2e0408](https://github.com/robinvdvleuten/beancount/commit/e2e040888657bb22686c9dfc3ac5593bc4d8fdbb)), closes [#395](https://github.com/robinvdvleuten/beancount/issues/395) [#396](https://github.com/robinvdvleuten/beancount/issues/396)

## [0.11.0](https://github.com/robinvdvleuten/beancount/compare/v0.10.0...v0.11.0) (2026-07-11)


### Features

* **formatter:** match bean-format column alignment exactly ([7b1488e](https://github.com/robinvdvleuten/beancount/commit/7b1488e744fa9eb6ed56d11b49987485091566c9))
* **ledger:** implement STRICT and NONE booking semantics ([73697e7](https://github.com/robinvdvleuten/beancount/commit/73697e7a770166f576f18fbceb46c5827e56a9ba))
* **ledger:** support HIFO booking ([b08b733](https://github.com/robinvdvleuten/beancount/commit/b08b733627a87e8d381b273b78bcfc746488d3d6))
* **loader:** generate document directives from the documents option ([ea77a16](https://github.com/robinvdvleuten/beancount/commit/ea77a16bdf7120fb9c9c923773a8dea94cf9c30a))
* **parser:** align lexer character classes with Beancount v2 ([7f9eb31](https://github.com/robinvdvleuten/beancount/commit/7f9eb314a13512abe812647a7ad394e934dae9c9))
* **parser:** evaluate number expressions at parse time ([9a4ece3](https://github.com/robinvdvleuten/beancount/commit/9a4ece34b0cc99eb07e70c054824c7d7104685f2))
* **parser:** support compound per-unit and total costs ([160d1e8](https://github.com/robinvdvleuten/beancount/commit/160d1e89880251159422a51038f403143e42eb34))
* **parser:** support date and label components in cost specs ([4c5c21a](https://github.com/robinvdvleuten/beancount/commit/4c5c21a771f1f51037a054605d09a49148b6e039))
* **parser:** support incomplete amounts and price interpolation ([23be333](https://github.com/robinvdvleuten/beancount/commit/23be3332eb51b2af15f9fbe8f5d76e24f7b3ce76))
* **query:** implement bean-query (BQL) with byte parity against beancount v2 ([#290](https://github.com/robinvdvleuten/beancount/issues/290)) ([768e3d3](https://github.com/robinvdvleuten/beancount/commit/768e3d3dd863b5db424edcc8067f1d0c9687c5bb))


### Bug Fixes

* **ast:** match official beancount same-date directive ordering ([c3e8b5b](https://github.com/robinvdvleuten/beancount/commit/c3e8b5bafc60d9a22f5b81de60d18e60b3e6933c))
* **config:** accumulate operating_currency as a list ([cf6d11a](https://github.com/robinvdvleuten/beancount/commit/cf6d11aacad60c025e7e43f26f89066be51c3690))
* **config:** reject unknown option names like official beancount ([8a93170](https://github.com/robinvdvleuten/beancount/commit/8a93170ae0e116eb5d22eebc33ec23cf7bb8128c))
* **diagnostic:** classify wrapped errors with errors.As ([f8b575f](https://github.com/robinvdvleuten/beancount/commit/f8b575f739200fdbe1ae86ace9c5dafd901f6e18))
* **formatter:** keep empty narration when payee is present ([76d8320](https://github.com/robinvdvleuten/beancount/commit/76d8320700e7658d2350ff14a7fe9451b4d45111))
* **formatter:** match bean-format indentation behavior ([5b27dd1](https://github.com/robinvdvleuten/beancount/commit/5b27dd1dd075abaf9be34c3cac5fde5fd6b2ac03))
* **ledger:** allow balance, note and document directives after close ([a28f489](https://github.com/robinvdvleuten/beancount/commit/a28f4895b26a129b2acb880ab8f2b8cefe63aa96))
* **ledger:** double the multiplier for inferred balance assertion tolerance ([4ce37b1](https://github.com/robinvdvleuten/beancount/commit/4ce37b1c32ff938d2d285546a68de015261959dc))
* **ledger:** infer transaction tolerance from coarsest precision ([6ba6619](https://github.com/robinvdvleuten/beancount/commit/6ba6619ccd6875c12a79e4c20ec7e50d52ea1c97))
* **ledger:** remove non-standard global 0.005 tolerance default ([3fb7649](https://github.com/robinvdvleuten/beancount/commit/3fb76490134ca6f8212cb59b8eac75569e9efb22))
* **ledger:** report superseded pads as unused like official beancount ([523603c](https://github.com/robinvdvleuten/beancount/commit/523603c9b00cae2d20a7e443e4be89000ee2f11c))
* **ledger:** resolve empty-spec reduction weights from booked lots ([a7e5932](https://github.com/robinvdvleuten/beancount/commit/a7e59328a78d258fc5d1cf38d425ebc23ddaf9dc))
* **ledger:** use official beancount booking_method vocabulary and STRICT default ([9600e92](https://github.com/robinvdvleuten/beancount/commit/9600e927f6865b4a114600b96d4312255df58ccb))
* **ledger:** verify document files exist like official beancount ([5658e6a](https://github.com/robinvdvleuten/beancount/commit/5658e6af709f5db33a6f4a88f1aa2432a09225c6))
* **loader:** warn about options in included files ([bdedec2](https://github.com/robinvdvleuten/beancount/commit/bdedec23512182742b8bfba453730a1f2ebc88d8))
* **parser:** accept tags and links on document directives ([0d72c45](https://github.com/robinvdvleuten/beancount/commit/0d72c4568df0f371978d61b2065f77b7c4a5219b))
* **parser:** enforce line-scoped grammar elements found by fuzzing ([dcecffa](https://github.com/robinvdvleuten/beancount/commit/dcecffaa15324aefa27d94f81dcebced25601310))

## [0.10.0](https://github.com/robinvdvleuten/beancount/compare/v0.9.0...v0.10.0) (2026-06-22)


### Features

* add route for viewing balance sheet ([#245](https://github.com/robinvdvleuten/beancount/issues/245)) ([0530861](https://github.com/robinvdvleuten/beancount/commit/053086113f425ad55f3448b4a28f0ddba885d6c1))
* **editor:** save with Cmd+S keyboard shortcut ([#211](https://github.com/robinvdvleuten/beancount/issues/211)) ([eb35fcd](https://github.com/robinvdvleuten/beancount/commit/eb35fcdbacf0886e87dc0d3393bee0f0be081795)), closes [#179](https://github.com/robinvdvleuten/beancount/issues/179)
* **formatter:** preserve transaction body trivia ([11c8ac7](https://github.com/robinvdvleuten/beancount/commit/11c8ac7504a9e6b46480ade0ead80d60ddc55a7b))
* **parser:** improve syntax error diagnostics ([5d7be7c](https://github.com/robinvdvleuten/beancount/commit/5d7be7c02bab1abb3c163699890bf20bb859815e))
* **parser:** keep parsing syntax-only ([1e68a78](https://github.com/robinvdvleuten/beancount/commit/1e68a78fe38adead75748ba5b82e3764e5f6c3c8))
* **parser:** tighten beancount syntax parsing ([c4d5718](https://github.com/robinvdvleuten/beancount/commit/c4d571887a325c7e1d2e98e7fb7701ae0e2f7f6f))
* preserve decimal balance precision in API and UI ([#246](https://github.com/robinvdvleuten/beancount/issues/246)) ([26f4cf7](https://github.com/robinvdvleuten/beancount/commit/26f4cf709284f505c56331add59db51e891b70b3))


### Bug Fixes

* **editor:** autocomplete non-leading account segments ([#212](https://github.com/robinvdvleuten/beancount/issues/212)) ([914a37c](https://github.com/robinvdvleuten/beancount/commit/914a37c22854c974673d465aaa4558ffba479f84)), closes [#176](https://github.com/robinvdvleuten/beancount/issues/176)
* **editor:** close file selector on outside click ([b758f78](https://github.com/robinvdvleuten/beancount/commit/b758f786129a7fef1932efe8dc977842df49056c))
* **formatter:** keep multiline string formatting idempotent ([04b177f](https://github.com/robinvdvleuten/beancount/commit/04b177f13659259fdee7706e888a2f43313475ac))
* **ledger:** convert the requested amount ([d757f7f](https://github.com/robinvdvleuten/beancount/commit/d757f7f12c57ec7d2eaf637c3c8be2ecc07dbe29)), closes [#249](https://github.com/robinvdvleuten/beancount/issues/249)
* **ledger:** handle invalid padding transactions ([25397c7](https://github.com/robinvdvleuten/beancount/commit/25397c786da3c95e8d77d8ce49766b77bb944c9c)), closes [#251](https://github.com/robinvdvleuten/beancount/issues/251)
* **ledger:** honor LIFO lot booking ([65855f4](https://github.com/robinvdvleuten/beancount/commit/65855f4db8f436aaac3513f306eba907842a8616)), closes [#247](https://github.com/robinvdvleuten/beancount/issues/247)
* **ledger:** isolate BFS path slices ([3ad22cf](https://github.com/robinvdvleuten/beancount/commit/3ad22cfb70378a968244b838b34c64dd80202f14)), closes [#248](https://github.com/robinvdvleuten/beancount/issues/248)
* **ledger:** reject mixed nil dates in GetBalanceTree ([#267](https://github.com/robinvdvleuten/beancount/issues/267)) ([b0c5363](https://github.com/robinvdvleuten/beancount/commit/b0c53630b6ed6610d1c298887a8c8fd734299b8a))
* **ledger:** resolve review sweep findings ([1cf9a0d](https://github.com/robinvdvleuten/beancount/commit/1cf9a0d8a4fd04205d17889625d282d581963237)), closes [#260](https://github.com/robinvdvleuten/beancount/issues/260)
* **ledger:** upgrade implicit account nodes ([920963b](https://github.com/robinvdvleuten/beancount/commit/920963b5862425e087d3ad1c8c74cd558fe72718))
* **web:** report saved source errors and allow startup ([#209](https://github.com/robinvdvleuten/beancount/issues/209)) ([c8b66bd](https://github.com/robinvdvleuten/beancount/commit/c8b66bda0a22eb99638f5c497826fa7381f7ef1d)), closes [#178](https://github.com/robinvdvleuten/beancount/issues/178)
* **web:** stop server on context cancellation ([4f98c75](https://github.com/robinvdvleuten/beancount/commit/4f98c75bd31d358690a27ed2c9644c2db4f5cd1b)), closes [#259](https://github.com/robinvdvleuten/beancount/issues/259)


### Performance Improvements

* **ledger:** cache price graphs by date ([281d4a3](https://github.com/robinvdvleuten/beancount/commit/281d4a3659c83272d44352cb86fd0ba664d6a048)), closes [#253](https://github.com/robinvdvleuten/beancount/issues/253)
* **ledger:** index incoming graph edges ([1751e58](https://github.com/robinvdvleuten/beancount/commit/1751e58a3a68d5cfc33eea69d984142477f41a81)), closes [#255](https://github.com/robinvdvleuten/beancount/issues/255)
* **ledger:** index price dates incrementally ([6eaadaf](https://github.com/robinvdvleuten/beancount/commit/6eaadaf640ab6d0e434aef4546a34a529e8ee9ce)), closes [#254](https://github.com/robinvdvleuten/beancount/issues/254)
* **ledger:** reuse the account lookup map ([e15e0bb](https://github.com/robinvdvleuten/beancount/commit/e15e0bbc0acbd2c0e5cac1790afc120f0792b383)), closes [#252](https://github.com/robinvdvleuten/beancount/issues/252)

## [0.9.0](https://github.com/robinvdvleuten/beancount/compare/v0.8.0...v0.9.0) (2026-03-31)


### Features

* **web:** auto reload on changes through --watch flag ([#121](https://github.com/robinvdvleuten/beancount/issues/121)) ([4e28b10](https://github.com/robinvdvleuten/beancount/commit/4e28b1007d8e40b976466f01e6a5b0e9eaad20d2))


### Bug Fixes

* **ci:** remove duplicate govulncheck tags ([15542bd](https://github.com/robinvdvleuten/beancount/commit/15542bda91f21412c7b4be115a57712a165d667c))
* **cli:** add missing error context for Price/Event/Custom/Commodity ([41a55c7](https://github.com/robinvdvleuten/beancount/commit/41a55c7589db36fc3186f325679b55565bcc6f36))
* **editor:** preserve updateListener in reconfigure to fix empty save ([f52d0ba](https://github.com/robinvdvleuten/beancount/commit/f52d0badceedc83e1b7fbc7361694b1e5625aa6d))
* **formatter:** preserve inline comments on price directives ([60185bc](https://github.com/robinvdvleuten/beancount/commit/60185bca6b4ef9ebefcf33ab9982bc5ec1c9be24))
* **formatter:** split source lines on `\r` and `\r\n` to match lexer ([444f5ae](https://github.com/robinvdvleuten/beancount/commit/444f5aeb7db8c6e0152d9fbc52aa21a41b8ec300))
* **ledger:** move pad-used marking from Validate to Apply ([bbab4f2](https://github.com/robinvdvleuten/beancount/commit/bbab4f247a50a0a3e2b8cf8a602620429c8bb3c7))
* **ledger:** prevent division by zero in empty cost inference ([05104b5](https://github.com/robinvdvleuten/beancount/commit/05104b5c78d323f23a30dae99cb1b748f7cfc62a))
* **ledger:** remove double counting of inferred amounts in tolerance ([d664b1a](https://github.com/robinvdvleuten/beancount/commit/d664b1a8e670230151af0847a9aaed164fc64f29))
* **ledger:** replace dead code in cost label validation ([25179ce](https://github.com/robinvdvleuten/beancount/commit/25179cefa3bc87dcd3408dc6174026265374ec52))
* **lexer:** prevent newline consumption in malformed strings ([913b29e](https://github.com/robinvdvleuten/beancount/commit/913b29e1277bdf522dbcf452924c0dc0bfa0b49a))
* **lexer:** support slash dates and multiline strings ([3022516](https://github.com/robinvdvleuten/beancount/commit/3022516218985f913c075bc22a0da2d389619ac5))
* **parser:** accept documented top-level and txn forms ([bfbe22d](https://github.com/robinvdvleuten/beancount/commit/bfbe22d4b20b8675de8641ab42af8e74744818e0))
* **parser:** accept org headers and open metadata ([0a3625f](https://github.com/robinvdvleuten/beancount/commit/0a3625fd818107f6652724c73dc1d7f4474f4d90))
* **parser:** add same-line check for currency in custom amounts ([f54ff07](https://github.com/robinvdvleuten/beancount/commit/f54ff0764e1ce5a2e1bb869f008c294c41cad2e4))
* **parser:** align with beancount and add query support ([54d3932](https://github.com/robinvdvleuten/beancount/commit/54d39325f97e5309522fd866674993bf02222ed0))
* **parser:** handle ACCOUNT tokens in custom directive values ([a3e4ff7](https://github.com/robinvdvleuten/beancount/commit/a3e4ff7e5628c35ccc5c71d5c239834fe1b3ac27))
* **parser:** propagate errors from parseMetadataValue ([7b369de](https://github.com/robinvdvleuten/beancount/commit/7b369de91ec2145196d7f4dcb070f083c6ae8b60))
* **parser:** remove incorrect look-back in escape sequences ([ebb8f65](https://github.com/robinvdvleuten/beancount/commit/ebb8f6535f12cb1c73b498d1c27724632175ce8f))
* **parser:** remove ineffective break in custom value switch ([098eca5](https://github.com/robinvdvleuten/beancount/commit/098eca56ecfe962c13a29916996d2e2e8ac11400))
* **parser:** return clear error on EOF after date token ([f9d867b](https://github.com/robinvdvleuten/beancount/commit/f9d867beb5e7f3a591d12837b22628c3fe24732d))
* **parser:** return error on unmatched parentheses in expressions ([78be9d5](https://github.com/robinvdvleuten/beancount/commit/78be9d51f3d36e0a10377ea7b035f5885254447c))
* **parser:** return ILLEGAL token for unterminated strings ([82a14c9](https://github.com/robinvdvleuten/beancount/commit/82a14c96e47708ef0f954efd2aa767a6673cf161))
* **parser:** store non-boolean IDENTs as string in custom values ([ed09117](https://github.com/robinvdvleuten/beancount/commit/ed09117ca4256897fbc4413f3aa49a768b580965))
* **parser:** support documented directive syntax variants ([b5e6134](https://github.com/robinvdvleuten/beancount/commit/b5e61341696f0993a071e2d86cdb1d9c78c9675e))
* **parser:** support documented metadata and custom values ([803ec37](https://github.com/robinvdvleuten/beancount/commit/803ec379a155a3a27fa4c1735077dbd61287945a))
* upgrade Go toolchain to 1.24.11 for crypto/x509 security fixes ([2e55c43](https://github.com/robinvdvleuten/beancount/commit/2e55c43abb897e1962a0878d4a28f7bb98c61806))
* **web:** eliminate race condition in handleFileChange ([742b09e](https://github.com/robinvdvleuten/beancount/commit/742b09ef511d8b55ef615f5b110457dccaccb0ef))
* **web:** return response instead of 500 when reload fails after save ([88867ec](https://github.com/robinvdvleuten/beancount/commit/88867ecbe22557d9bec612c880b660af629b101f))


### Performance Improvements

* **lexer:** use map lookup for keyword type resolution ([94458eb](https://github.com/robinvdvleuten/beancount/commit/94458ebb10df404190062550d3ee925daf3aa045))
* **parser:** avoid string conversion in calculateSourceRange ([2d5a35d](https://github.com/robinvdvleuten/beancount/commit/2d5a35d92755f1788e1b33207d73663f06d7c8b8))
* **parser:** use strings.Builder in parseRestOfLine ([b24a5ec](https://github.com/robinvdvleuten/beancount/commit/b24a5ec59a15c0a05b9bc45c4d17be890e267ddb))

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
