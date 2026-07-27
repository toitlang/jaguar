# SDK 196 peripheral migration

Tracking the package releases required by the SDK peripheral API change from
`gpio.Pin` objects to integer GPIO numbers.

## Existing migration branches

Each repository below started with:

- `floitsch/fix-gpio` containing the source or call-site migration.
- `floitsch/fix-gpio.ci` containing the post-SDK-release constraint update.

Target state:

- [x] `floitsch/fix-gpio` includes `environment.sdk: ^2.0.0-alpha.196`.
- [x] The useful changes from `floitsch/fix-gpio.ci` are retained.
- [x] `floitsch/fix-gpio.ci` is removed.

| Repository | SDK 196 on `fix-gpio` | `.ci` removed | Publish workflow | Notes |
| --- | --- | --- | --- | --- |
| LIS3DH | Done | Done | Present | Example/call-site migration. |
| bme280-driver | Done | Done | Present | I2C provider migration. |
| bno055 | Done | Done | Present | Example/call-site migration. |
| cellular | Done | Done | Present | UART service and module migration. |
| icm20948-driver | Done | Done | Present | Example/call-site migration. |
| max31865-driver | Done | Done | Present | SPI provider migration. |
| mcp2518fd-driver | Done | Done | Present | SPI and interrupt-pin migration. |
| mcp342x-driver | Done | Done | Present | Example/call-site migration. |
| sts3x-driver | Done | Done | Present | I2C provider migration. |
| toit-1-wire | Done | Done | Present | RMT and public pin API migration. |
| toit-color-tft | Done | Done | Present | Example/call-site migration. |
| toit-dhtxx | Done | Done | Present | RMT and public pin API migration. |
| toit-ds18b20 | Done | Done | Present | Rebased onto merged `origin/main` dependency update. |
| toit-e-paper | Done | Done | Present | Reset/busy API and ownership migration. |
| toit-es8388 | Done | Done | Present | Example I2C/I2S migration. |
| toit-hc-sr04 | Done | Done | Alternate `update.yml` | Existing package-publish workflow retained. |
| toit-heading | Done | Done | Alternate `update.yml` | Existing package-publish workflow retained. |
| toit-hx711 | Done | Done | Present | Clock/data API and ownership migration. |
| toit-ibutton | Done | Done | Present | One-wire API migration. |
| toit-lsm303d | Done | Done | Added | Canonical `publish.yml` added on `fix-gpio`. |
| toit-lsm303dlh | Done | Done | Alternate `update.yml` | Existing package-publish workflow retained. |
| toit-lsm303dlhc | Done | Done | Alternate `update.yml` | Existing package-publish workflow retained. |
| toit-m5stack-core2 | Done | Done | Present | Internal buses and Power API migration. |
| toit-msa311 | Done | Done | Added | Canonical `publish.yml` added on `fix-gpio`. |
| toit-pixel-strip | Done | Done | Present | UART, I2S, and RMT migration. |
| toit-qwiic-joystick | Done | Done | Present | Example/call-site migration. |
| toit-rs485 | Done | Done | Present | UART and enable-pin API migration. |
| toit-ssd1306 | Done | Done | Present | Reset-pin API and ownership migration. |
| toit-vcnl4040 | Done | Done | Present | Example/call-site migration. |
| ublox-gnss-driver | Done | Done | Present | Example I2C/UART migration. |

## Jaguar repository

Jaguar itself was handled after the sibling-package pass:

- [x] Rebased `floitsch/fix-gpio` onto current `origin/main`.
- [x] Retained the UART integer-pin migration.
- [x] Added `environment.sdk: ^2.0.0-alpha.196` on `floitsch/fix-gpio`.
- [x] Removed the local `floitsch/fix-gpio.ci` branch.
- [x] Left Jaguar's application release workflows unchanged.

## Other missing publish workflows

The following package-shaped repositories have no `publish.yml`, `publish.yaml`,
or alternate package-publish workflow. Add the canonical `publish.yml` from the
`toit-package` skill.

- [x] DHT11 — `floitsch/add-publish-workflow`
- [x] Rail — `floitsch/add-publish-workflow`
- [x] artemis — `floitsch/add-publish-workflow`
- [x] chats — `floitsch/add-publish-workflow`
- [x] jaguar-test — `floitsch/add-publish-workflow`
- [x] toit-hue — `floitsch/add-publish-workflow`
- [x] toit-lsm303d — `floitsch/fix-gpio`
- [x] toit-morse-tutorial — `floitsch/add-publish-workflow`
- [x] toit-msa311 — `floitsch/fix-gpio`
- [x] toit-rn4871 — `floitsch/fix-gpio`

The following repositories already had an alternate package-publish workflow,
`update.yml`, using `toitlang/pkg-publish@v1.0.2`; these were retained:

- `toit-hc-sr04`
- `toit-heading`
- `toit-lsm303dlh`
- `toit-lsm303dlhc`

## Additional migration candidates

| Repository | Initial cleanliness | Default branch update | Migration branch | Notes |
| --- | --- | --- | --- | --- |
| bmp180-driver | Clean | Up to date | Excluded | Personal upstream `davidlao2k/bmp180-driver`; not part of this release. |
| bmp280-driver | Clean | Up to date | Excluded | Personal upstream `harshkc03/bmp280-driver`; not part of this release. |
| toit-rn4871 | Clean | Fast-forwarded | Ready | Migrated UART/reset API and example; added ownership cleanup and publish workflow. |

The prepared local BMP migration branches are retained for reference, but they
must not be published or included in this release.

## Verification

- [x] All three additional migration worktrees are clean.
- [x] All 32 in-scope `floitsch/fix-gpio` branches, including Jaguar, have SDK floor `^2.0.0-alpha.196`.
- [x] No local `floitsch/fix-gpio.ci` branches remain in the tracked repositories.
- [x] All 10 in-scope added workflows exactly match the `toit-package` canonical resource.
- [x] The prepared extra migrations analyzed successfully with SDK `v2.0.0-alpha.196` before the BMP packages were excluded.

RN4871 retains pre-existing analyzer warnings about attached parentheses and
deprecated `int.stringify`; the GPIO migration itself introduces no analyzer
errors. `toit pkg describe` also reports that RN4871 has no recognized package
license.
