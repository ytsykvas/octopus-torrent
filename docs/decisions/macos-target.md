# Цільова macOS і ідентичність застосунку

**Статус:** `accepted` (мінімальну версію переглядає [066](../../tasks/066-personal-build-install-license-audit.md))  
**Дата:** 2026-10-14  
**Задача:** [003](../../tasks/003-wails-macos-build-spike.md)  
**Пункт реєстру:** O1 із [базового опису](project-baseline.md#6-реєстр-відкритих-рішень)  
**Вимоги:** REL-01, REL-02, STACK-05

## Питання

Яку версію macOS і архітектуру Mac застосунок заявляє підтримуваними та з яким ідентифікатором пакета він реєструється в системі. Ідентифікатор потрібен надовго: типовий застосунок для `.torrent`, папка даних, дозволи macOS.

## Рішення

- **Підтримувана конфігурація — тільки перевірена:** macOS 27.0.1 (26A434), Apple Silicon `arm64` (Apple M4). `LSMinimumSystemVersion` = `27.0` у `build/darwin/Info.plist` та `Info.dev.plist`. Старіші версії macOS і Intel Mac не заявляються, доки їх не перевірено.
- **Архітектура пакета:** лише `arm64` (`darwin/arm64`, типова ціль `wails build` на цьому Mac). Universal-збірка не потрібна (вимоги §1).
- **Bundle identifier:** `dev.tsykvas.octopustorrent` — рішення власника від 2026-10-14. Шаблон Wails `com.wails.<name>` замінено в обох plist.
- **Підпис:** ad-hoc, автоматично під час `wails build` («Self-signing application»); `TeamIdentifier` не задано. Gatekeeper (`spctl -a`) відхиляє такий пакет, але локально зібрана `.app` не має атрибута карантину й відкривається з Finder. Developer ID і notarization не потрібні для особистої збірки (REL-02) і розглядаються в 066.

## Розглянуті альтернативи

| Варіант | Переваги | Недоліки | Чому не обрано |
|---|---|---|---|
| Типове значення Wails `LSMinimumSystemVersion 10.13.0` | Нічого не змінювати | Заявляє сумісність, яку ніхто не перевіряв; Go 1.27 вимагає новішу macOS | Порушує правило «неперевірене не називається підтримуваним» |
| Universal (`arm64` + `amd64`) | Працює й на Intel Mac | Довша збірка, неперевірена ціль | Інших Mac немає в обсязі |
| `com.wails.OctopusTorrent` | Значення за замовчуванням | Простір імен не належить власнику | Рішення власника — `dev.tsykvas.octopustorrent` |

## Перевірені підстави

[Звіт 003](../verification/003-wails-macos-build-spike.md): `plutil -p` показує `CFBundleIdentifier = dev.tsykvas.octopustorrent` і `LSMinimumSystemVersion = 27.0`; `lipo -archs` → `arm64`; `codesign -dv` → `Signature=adhoc`, `codesign --verify --deep --strict` — `pass`; `spctl -a` → `rejected`; запуск із Finder (подвійний клік власника) і через `open` — `pass`.

## Наслідки

- Під час встановлення на іншу версію macOS система відмовить у запуску, доки 066 не перевірить і не знизить мінімум.
- Зміна bundle ID після появи бібліотеки потребує міграції даних і повторного вибору типового застосунку, тому ID вважається сталим.
- `CFBundleDocumentTypes` для `.torrent` і схема `magnet:` додаються в ці plist у [036](../../tasks/036-finder-magnet-open-queue.md) (O13).
