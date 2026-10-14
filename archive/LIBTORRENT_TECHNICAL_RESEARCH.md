> **Архів попереднього стеку — 14 жовтня 2026.** Цей документ описує libtorrent, який більше не використовується в OctopusTorrent. Він збережений лише як довідка; API, збірка й рішення інтеграції тут не є актуальними вимогами. Обраний стек: React + TypeScript, Wails v2, Go, anacrolix/torrent і SQLite. Актуальний технічний план: [ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md).

# Libtorrent визначає технічний фундамент OctopusTorrent

**Висновок.** Для нативного OctopusTorrent доцільно використати **libtorrent-rasterbar 2.x як C++ рушій BitTorrent**, а SwiftUI залишити інтерфейсом і продуктовою логікою. Бібліотека вже виконує виявлення пірів, обмін блоками, перевірку хешів, дискове введення/виведення, роздачу та відновлення торрента; застосунок має керувати її сесією, перетворювати події на зрозумілий стан і надійно зберігати власний список. Найскладніші місця саме для OctopusTorrent — **асинхронний міст Swift–C++**, чесна семантика вибору окремого файла, збереження стану після збоїв, безпечне видалення та узгодження локальної БД із fast-resume. Перший технічний етап має зафіксувати конкретний тег libtorrent, довести роботу `.torrent` і magnet на власному Mac та перевірити локальне встановлення `.app`. Нижче наведено задокументовану поведінку бібліотеки та окремо позначено рішення, які пропонуємо для продукту. Стан дослідження: **10 жовтня 2026 року**; вебдовідник `*-ref.html` показує версію **2.1.0**, тоді як сторінка релізів містить новіші patch-релізи, тому точні сигнатури й поведінку треба повторно звірити із зафіксованим тегом перед написанням production-коду. ([Libtorrent manual](https://libtorrent.org/manual-ref.html)) ([Релізи libtorrent](https://github.com/arvidn/libtorrent/releases))

**Продуктовий обсяг оновлено 14 жовтня 2026 року:** локальний застосунок для особистого використання; технічні першоджерела досліджені 10 жовтня 2026 року.

## Рушій бере на себе протокол, але продукт визначає бібліотеку користувача

### Як рухаються дані

`.torrent` — це bencoded метадані: `info` описує файли, їхні розміри й хеші частин, але **не містить завантажених файлів**. Хеш `info` ідентифікує торрент у swarm. Клієнт знаходить учасників через tracker, розподілену таблицю DHT, обмін адресами PEX або локальне виявлення LSD; далі піри обмінюються відомостями про наявні pieces, запитують блоки й передають payload. Libtorrent керує мережевими з’єднаннями TCP/uTP, вибором рідкісних pieces, перевіркою цілісності й роздачею. За замовчуванням у налаштуваннях доступні DHT, LSD, UPnP і NAT-PMP, але конкретну політику для застосунку слід встановити й показати явно. **Tracker не передає вміст файла**: він лише допомагає знайти пірів. Помилка одного tracker також не означає, що торрент загалом не має жодного джерела. ([BEP 3](https://www.bittorrent.org/beps/bep_0003.html)) ([Можливості libtorrent](https://libtorrent.org/features-ref.html)) ([Налаштування libtorrent](https://libtorrent.org/reference-Settings.html))

У BitTorrent v1 хеш торрента використовує SHA-1, а перевірка спирається на хеші pieces. **BitTorrent v2** використовує SHA-256 та Merkle-дерева для файлів; hybrid-торрент може містити обидві форми й брати участь у відповідних swarms. Тому ідентичність у власній базі не можна моделювати одним обов’язковим 20-байтовим хешем. API 2.x має `info_hash_t`, `v1()` і `v2()`; після отримання метаданих magnet-запис може набути додаткового хешу й з’ясуватися дублікатом іншого запису. **Рекомендація:** локальний UUID — сталий ключ запису в UI, а v1/v2 хеші — окремі необов’язкові поля для зіставлення та локальної дедуплікації. Для hybrid потрібен тест на конфлікт ідентичностей після `metadata_received_alert`. ([BEP 52](https://bittorrent.org/beps/bep_0052.html)) ([Перехід libtorrent на 2.0](https://libtorrent.org/upgrade_to_2.0-ref.html)) ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html))

`magnet:` зазвичай містить info-hash, необов’язкову назву та tracker URL, але **не перелік файлів**. `parse_magnet_uri()` перетворює посилання на `add_torrent_params`; libtorrent одержує metainfo від пірів через extension protocol. До `metadata_received_alert` `torrent_handle::torrent_file()` може бути `nullptr`. Отже, для magnet інтерфейс повинен мати етап «Отримуємо відомості про файли» і не малювати вигаданий перелік чи точний загальний розмір. Метадані можуть узагалі не з’явитися, якщо немає доступних пірів; це окремий стан очікування або помилки, а не негайно «пошкоджений magnet». ([Core API](https://libtorrent.org/reference-Core.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Alerts API](https://libtorrent.org/reference-Alerts.html))

**Межа відповідальності.** Libtorrent виконує протокол і дискові операції; OctopusTorrent визначає колекції, папку спостереження, локальні правила, мову UI та безпечне видалення записів. Особиста версія не має штучного ліміту кількості торрентів. Валідація, дедуплікація та створення `pending import` мають бути узгоджені локально: одночасні drag-and-drop, Finder і watch folder не повинні створити дублікат. Усі точки входу використовують один ImportCoordinator. Це проєктне рішення, узгоджене з `PRODUCT_CONCEPT.md`, `PROJECT_REQUIREMENTS.md` та `APPLICATION_ARCHITECTURE.md`.

### Коротка карта API для команди

| Потреба OctopusTorrent | Основний API libtorrent 2.x | Важливе обмеження |
|---|---|---|
| Один рушій для всіх торрентів | `session`, `session_params`, `settings_pack` | Торренти не входять до серіалізованого стану сесії. |
| Додати `.torrent` | `load_torrent_file()` → `add_torrent_params` → `async_add_torrent()` | Парсинг і валідація до додавання. |
| Додати magnet | `parse_magnet_uri()` → `async_add_torrent()` | Файлів немає до `metadata_received_alert`. |
| Отримати стан | `post_torrent_updates()` → `state_update_alert` | Оновлюються торренти, що змінилися; UI зберігає попередній snapshot. |
| Пауза й старт | `torrent_handle::pause()` / `resume()` | Для суворої ручної паузи врахувати `auto_managed`. |
| Вибір файла | `file_priority()` / `prioritize_files()` | Пріоритет 0 не є атомарною «паузою файла». |
| Прогрес файла | `post_file_progress()` → `file_progress_alert` | Дорогий точний розрахунок; запитувати помірно. |
| Локальне відновлення | `save_resume_data()` → alert → `write_resume_data()` | Окремо для кожного торрента на цьому Mac. |
| Видалити запис | `session::remove_torrent(handle)` | `delete_files` передавати лише після окремого підтвердження. |

Сигнатури та назви вище походять із чинного вебдовідника 2.1.0; версії 1.2 і 2.x мають відмінності API/ABI. **Перед реалізацією зафіксувати конкретний тег і компілювати приклади саме з його заголовками**, а не копіювати уривки з пошуку. ([Session API](https://libtorrent.org/reference-Session.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Add torrent API](https://libtorrent.org/reference-Add_Torrent.html))

## Асинхронна сесія має віддавати SwiftUI знімки, а не C++ об’єкти

**Рекомендована архітектура:** один довгоживучий `session` у `TorrentEngine`, окремий послідовний worker для команд та зчитування alerts, вузький Objective-C++ або C API, Swift-сервіс із immutable DTO і SwiftUI на main actor. Це рекомендований дизайн, а не жорстка вимога libtorrent. Прямий Swift/C++ interop підтримується сучасним Xcode, але для першої версії вузький адаптер дає простіший контроль над C++ винятками, життям `torrent_handle`, потоками та стабільністю Swift API. Публічні методи моста мають приймати власні UUID й безпечні value-типи; C++ `session`, `torrent_handle`, `torrent_info`, pointers на alerts та `std::string_view` не повинні протікати у View. ([Apple: Swift і C++](https://developer.apple.com/videos/play/wwdc2023/10172/)) ([Session API](https://libtorrent.org/reference-Session.html))

`session` має власний мережевий потік. Чимало методів `torrent_handle`, які **повертають значення**, можуть чекати відповіді цього потоку; не викликати їх на SwiftUI main actor. `async_add_torrent()` повертає швидко, а `add_torrent_alert` приносить результат. `session::post_torrent_updates()` породжує `state_update_alert` для змінених торрентів. Callback `set_alert_notify()` повинен тільки розбудити наш worker; `pop_alerts()` викликається там. Отримані pointers належать сесії й чинні **лише до наступного `pop_alerts()`**, тому worker одразу копіює потрібні поля у власні DTO. Черга alerts обмежена, а `alerts_dropped_alert` означає потребу відновити повний snapshot і скоригувати частоту опитування/розмір черги. ([Session API](https://libtorrent.org/reference-Session.html)) ([Alerts API](https://libtorrent.org/reference-Alerts.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

**Ескіз життєвого циклу, не готовий для компіляції без звірки з вибраним тегом:**

```text
startup:
  прочитати локальну БД та перевірений локальний session state
  створити session із session_params/settings_pack
  для кожного запису відновити власний resume або metainfo
  async_add_torrent(params); зіставити add_torrent_alert із локальним UUID

import .torrent:
  load_torrent_file(path, limits); перевірити метадані і шляхи
  показати діалог файлів/папки; перевірити дублікати; застосувати file priorities
  async_add_torrent(params); за add_torrent_alert оновити БД/UI

import magnet:
  parse_magnet_uri(uri); створити контрольований metadata-only запис
  чекати metadata_received_alert; показати фактичний перелік файлів
  після підтвердження задати папку/пріоритети і дозволити payload

event loop:
  pop_alerts(); негайно скопіювати дані кожного alert
  обробити add/metadata/state/file/error/resume/remove події
  періодично post_torrent_updates(); окремо запитувати file progress для видимого UI
  опублікувати зведений snapshot на main actor

shutdown:
  асинхронно зберегти dirty resume кожного торрента; дочекатися результатів
  атомарно записати файли стану та окремий session_params
  завершити сесію без довільної зміни paused flags
```

Для отримання лише метаданих magnet документація описує `torrent_flags::upload_mode`: він не запитує payload, **але рушій може створити порожні файли**. Тому діалог попереднього перегляду слід прототипувати й тестувати на APFS; не обіцяти, що скасування magnet ніколи нічого не записувало. Після `metadata_received_alert` треба зберегти отриманий info dictionary, щоб наступний запуск не чекав на ті самі метадані. ([Manual libtorrent](https://libtorrent.org/manual-ref.html)) ([Core API](https://libtorrent.org/reference-Core.html)) ([Alerts API](https://libtorrent.org/reference-Alerts.html))

**Стан UI — похідний, а не прямий `torrent_status::state`.** Libtorrent розрізняє `checking_files`, `downloading_metadata`, `downloading`, `finished`, `seeding`, `checking_resume_data`; пауза — flag, а дискова помилка доступна через `errc`/alert. OctopusTorrent спершу перевіряє помилку, потім ручну/автоматичну паузу, перевірку, очікування метаданих, seeding, finished та downloading. `finished` може означати завершення лише **вибраних** файлів, тоді як `seeding` означає повний набір pieces. Статуси «Файли готові» і «Роздається» не слід ототожнювати. Для torrent-wide ручної паузи `auto_managed` потрібно вимкнути або спроєктувати політику черги так, щоб вона не запускала запис без команди користувача; це особливо важливо для автоматичного додавання з папки спостереження. ([Torrent status API](https://libtorrent.org/reference-Torrent_Status.html)) ([Core flags](https://libtorrent.org/reference-Core.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

| Подія | Значення для застосунку |
|---|---|
| `add_torrent_alert` | Підтвердити додавання або показати причину відмови; остаточно зв’язати handle й UUID. |
| `metadata_received_alert` / `metadata_failed_alert` | Побудувати список файлів або показати помилку метаданих, яка може бути тимчасовою. |
| `state_update_alert`, `state_changed_alert`, `torrent_finished_alert` | Оновити список, інспектор, прогрес та переходи статусів. |
| `file_prio_alert`, `file_progress_alert` | Підтвердити вибір файлів і оновити їхній прогрес. |
| `file_error_alert`, `torrent_error_alert`, `tracker_error_alert` | Відрізнити дисковий збій торрента від збою окремого tracker; локалізувати повідомлення. |
| `save_resume_data_alert` / `save_resume_data_failed_alert` | Завершити або повторити збереження локального стану. |
| `torrent_removed_alert`, `torrent_deleted_alert`, `torrent_delete_failed_alert` | Розвести видалення запису й фактичний результат видалення файлів. |
| `alerts_dropped_alert`, `listen_failed_alert` | Відновити актуальний стан або показати мережеву діагностику. |

Категорії alerts задаються `alert_mask`; не можна виходити з припущення, що стандартна маска дає весь потрібний UI-телеметричний потік. Повідомлення `error_code::message()` бібліотека не локалізує для шести мов OctopusTorrent; застосунок має зіставляти категорію та числовий код зі своїми текстами, зберігаючи технічний код у діагностиці. ([Alerts API](https://libtorrent.org/reference-Alerts.html)) ([Manual: переклад помилок](https://libtorrent.org/manual-ref.html))

## Файли, прогрес і відновлення потребують точнішої семантики за макет

### Імпорт, вибір файлів і папки

`load_torrent_file()` дає змогу розібрати `.torrent` **до додавання в сесію**. Для drag-and-drop розширення `.torrent` — лише швидкий фільтр; остаточна перевірка — успішний парсинг із `load_torrent_limits`, перевірка числа файлів, глибини каталогів, розміру метаданих, symlink, дубльованих і небезпечних шляхів, а також колізій у цільовій папці. `torrent_info::layout()` та `file_storage` дають перелік і розміри файлів. Діалог має показати папку, сумарний розмір **реальних, не pad-файлів**, вибір файлів і майбутні шляхи. Вибір/створення папки, security-scoped bookmark за App Sandbox та системна асоціація `.torrent` — рівень macOS, не libtorrent. ([Core API](https://libtorrent.org/reference-Core.html)) ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html)) ([Storage API](https://libtorrent.org/reference-Storage.html))

Керування завантаженням окремого файла не є паузою цілого торрента й не має однойменного API. Для окремого файла libtorrent використовує `file_priority(index, download_priority_t::dont_download)`; пріоритет 0 означає **«не завантажувати далі вибраний файл»**, а не миттєво зупинити кожен пов’язаний мережевий блок. У v1 одна piece може перетинати межу двох файлів; рушій може добрати boundary bytes. Зміна пріоритетів асинхронна й підтверджується `file_prio_alert`. Вже записані дані не зникають; у seed-стані пріоритет може не впливати на поточну роздачу, а до появи метаданих керування файлом недоступне. **Рішення для UI:** кнопки pause/play з діями «Не завантажувати» / «Завантажувати» і стан «Пропущено», із поясненням про вже отримані байти. Це вибір завантажуваних файлів, а не точна миттєва пауза. Повернення до завантаження відновлює збережений попередній пріоритет. Не змішувати без плану file priorities із piece priorities: зміна перших перераховує другі. ([Core API](https://libtorrent.org/reference-Core.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Storage API](https://libtorrent.org/reference-Storage.html))

### Дерево папок поверх плаского списку libtorrent

`file_storage` дає файли з відносними шляхами та стабільними індексами, а не окремі об’єкти папок. UI будує дерево із компонентів шляху, зберігаючи індекс кожного листка для `file_priority()` та прогресу. Не дублюємо кореневу назву торрента в дереві. Перед показом і записом перевіряємо абсолютні шляхи, `..`, symlink і колізії; pad-файли приховуємо з вибору та продуктових підсумків. Згортання папки — лише стан інтерфейсу. ([Storage API](https://libtorrent.org/reference-Storage.html))

У вже доданому торренті кнопка play/pause папки змінює вибір **усіх дочірніх файлів**, включно з готовими й файлами вкладених папок. Клієнт зберігає вибір кожного листка та застосовує його через `file_priority()` за індексом; наявні байти не видаляються, а виключення завершеного файла не гарантує миттєвого припинення роздачі. Окремої «паузи папки» в libtorrent немає. Коли вибрано всі дочірні файли, pause виключає їх; за нульового або часткового вибору видно play, що вибирає все. У діалозі до додавання для цього лишається чекбокс із проміжним станом. Прогрес папки — сума наявних байтів / сума розмірів видимих реальних нащадків, а кількість вибраних файлів рахується окремо; обидва підсумки не залежать від згортання. Для великих дерев будувати вузли один раз із metainfo, оновлювати лише видимі рядки й перевірити продуктивність на тисячах файлів. ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

### Одна формула для прогресу

Для картки завантаження вибраних файлів найпослідовніша пара — `total_wanted_done / total_wanted`. `total_payload_download` — лічильник переданих байтів, а не кількість корисних байтів, наявних зараз; `total_size()` може включати pad files. `download_payload_rate` та `upload_payload_rate` показують payload байти за секунду; `download_rate`/`upload_rate` також включають службовий протокольний трафік. Відсоток і «4.2 ГБ із 5.9 ГБ» треба рахувати **з однієї пари полів і однаково округляти**, інакше макет дасть суперечливі цифри. Якщо всі файли пропущені й `total_wanted = 0`, показати «Не вибрано файлів», а не 100%. ETA є власною приблизною оцінкою `(total_wanted − total_wanted_done) / payload_rate`; при нульовій швидкості, паузі чи перевірці її краще приховати. ([Torrent status API](https://libtorrent.org/reference-Torrent_Status.html)) ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html))

Для кожного файла існує `post_file_progress()` і `file_progress_alert`. Точний `file_progress()` відносно дорогий; режим `piece_granularity` дешевший і рахує лише повністю перевірені pieces. **Рекомендація:** запитувати дані для видимого розгорнутого торрента й інспектора, після `file_prio_alert` або через виміряний інтервал; не для всіх файлів усіх торрентів на кожному кадрі UI. Початкові 1–2 оновлення списку за секунду — лише гіпотеза для вимірювання, не гарантія libtorrent. Окремо виміряти пам’ять, CPU й час відповіді при сотнях та тисячах файлів. ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Session API](https://libtorrent.org/reference-Session.html))

### Два різні шари збереження

`session::session_state()`/`write_session_params_buf()` зберігають глобальні параметри, зокрема DHT/settings, **але не перелік торрентів**. Кожен торрент має окремий fast-resume: `save_resume_data()` асинхронно повертає `add_torrent_params` через alert, який можна серіалізувати `write_resume_data()`; при старті його читають `read_resume_data()`. Resume містить локальну інформацію про шляхи, отримані pieces, paused state і file priorities. Синхронний `get_resume_data()` не слід викликати з UI-потоку. Для magnet після появи metadata варто зберегти info dictionary. При некоректному resume можливий `fastresume_rejected_alert` і повна перевірка файлів. **Рекомендація:** писати resume атомарно, зі збереженням попередньої копії або транзакційною заміною; відстежувати dirty state й зберігати після змін, при паузі/завершенні та на shutdown, але не відкладати все до закриття застосунку. ([Session API](https://libtorrent.org/reference-Session.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Resume data API](https://libtorrent.org/reference-Resume_Data.html)) ([Manual: fast resume](https://libtorrent.org/manual-ref.html))

**Resume належить конкретному Mac і конкретним файлам.** Він містить локальний `save_path`, pieces, пріоритети й паузу; документація вимагає захищати resume-файл від недовіреної зміни. Особиста версія зберігає його локально. БД тримає UUID та повні v1 SHA-1/v2 SHA-256, metainfo або magnet і локальні теги/правила; обрізаний v2-хеш не є універсальним ключем. Якщо файли або шлях змінилися, потрібна перевірка й явне рішення користувача, а не довіра до застарілих даних resume. ([Resume data API](https://libtorrent.org/reference-Resume_Data.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

### Видалення та переміщення

`session::remove_torrent(handle)` без спеціальних flags вилучає запис із сесії, залишаючи файли. `delete_files` видаляє файли торрента разом із part-file, `delete_partfile` — лише part-file. Для OctopusTorrent типова дія має бути **«Видалити зі списку, файли залишити»**. Окреме «Видалити торрент і файли» вимагає підтвердження з переліком шляхів та явного `delete_files`; результат фізичного видалення треба простежити за `torrent_deleted_alert` або `torrent_delete_failed_alert`, бо вилучення з сесії й видалення з диска — різні результати. Для bulk delete діє та сама вимога для кожного виділеного торрента. Після remove не користуватися старим handle: він стає невалідним. ([Session API](https://libtorrent.org/reference-Session.html)) ([Alerts API](https://libtorrent.org/reference-Alerts.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

Перед `delete_files` варто додатково виявляти спільні фізичні файли, сторонні файли в папці, перейменовані або абсолютні шляхи та symlink: API не формулює для цих випадків продуктової гарантії «видаляться тільки показані файли». Якщо точний перелік не можна довести, безпечніше пропонувати залишити payload або вимагати окремої перевірки. Звичайний remove без `delete_partfile` може залишити технічний part-file; політика його очищення має бути явною й протестованою, особливо коли користувач очікує зберегти можливість швидко відновити торрент. ([Session API](https://libtorrent.org/reference-Session.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

`move_storage()` для іншого диска може копіювати й видаляти старі файли; результат асинхронний, а типовий режим може перезаписувати target. `rename_file()` також асинхронний, а `torrent_info::layout()` лишається початковим після перейменування. Тому при зміні папки вже активного торрента потрібно показувати окрему операцію з прогресом/помилкою, перевірити цільову папку та обережно вибрати режим заміни. Налаштування **типової папки для майбутніх завантажень** не повинне непомітно переміщувати вже наявні файли. ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Storage API](https://libtorrent.org/reference-Storage.html))

Для першого прототипу достатньо рекомендованого `storage_mode_sparse` та стандартного disk I/O backend. Інший backend або full allocation має сенс лише після вимірювання APFS, зовнішнього диска й одночасного навантаження; libtorrent не дає готового оптимального профілю для OctopusTorrent. Перенесення між томами слід тестувати на переривання та нестачу місця, бо копіювання може затримати інші дискові операції. ([Manual: storage allocation](https://libtorrent.org/manual-ref.html)) ([Storage API](https://libtorrent.org/reference-Storage.html))

### Глобальний режим без віддачі потребує окремого прототипу

Запит користувача — **припинити передачу payload pieces для всіх торрентів**, залишивши активні завантаження. Це не те саме, що призупинити лише завершені торренти: під час завантаження клієнт теж віддає вже отримані pieces. Просте `settings_pack::upload_rate_limit = 0` не підходить: **0 означає без обмеження**, а дуже малий ліміт може обмежити й службові повідомлення, потрібні для завантаження. `active_seeds = 0` керує чергою завершених торрентів, але не доводить нульову віддачу активних завантажень. `torrent_flags::upload_mode` означає протилежне — завантаження payload припиняється. ([Settings API](https://libtorrent.org/reference-Settings.html)) ([Core API](https://libtorrent.org/reference-Core.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

Потрібен spike на зафіксованій версії libtorrent: перевірити керування unchoke та peer-класами або вузьке розширення рушія, відокремити payload від протокольного трафіку, виміряти `upload_payload_rate`, bytes sent у контрольованому swarm і підтвердити, що download продовжується. `unchoke_slots_limit` не можна вважати універсальною гарантією: поведінка залежить від choking algorithm. Протестувати звичайні й приватні трекери, локальних пірів, нові та вже активні торренти, перезапуск і вимкнення режиму. Якщо точна поведінка недосяжна, продукт має показувати чесну альтернативу — обмеження швидкості віддачі або автоматичну зупинку роздачі після завершення — без назви «без віддачі». ([Settings API](https://libtorrent.org/reference-Settings.html)) ([Manual: choking](https://libtorrent.org/manual-ref.html))

## Мережа, безпека й дистрибуція формують межі першого релізу

Libtorrent відкриває вхідний порт і за стандартними налаштуваннями використовує механізми DHT/LSD та port mapping; LSD може оголосити info-hash у локальній мережі. Перемикачі проксі для peer-з’єднань, tracker, DNS і DHT охоплюють різні потоки. `anonymous_mode` лише частково змінює видимі ідентифікатори клієнта; protocol encryption **не дорівнює VPN чи гарантії анонімності**. Приватні торренти слід тестувати з DHT/PEX/LSD згідно з їхнім private-прапорцем і правилами tracker. **Рекомендація:** у першому релізі мати документовані фактичні defaults, діагностику порту й tracker, а для майбутнього proxy/VPN-only режиму окремо тестувати DNS, UDP, перехід між мережами та fail-closed поведінку. ([Settings API](https://libtorrent.org/reference-Settings.html)) ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html)) ([Mozilla security assessment of libtorrent](https://libtorrent.org/2020%20Q4%20Mozilla%20Libtorrent%20Report%20Public%20Report.pdf))

Вхідні `.torrent` і magnet — недовірені дані. Парсеру слід задавати межі (`load_torrent_limits`, `max_metadata_size`), а власним файловим діям — перевіряти нормалізовані шляхи, symlink, collision і достатнє місце. Правило watch folder має реагувати лише на завершений запис `.torrent`, перевіряти вміст парсером і дублікати, не видаляти джерельний файл після збою. **Рекомендований стандарт:** за замовчуванням показувати діалог попереднього перегляду; auto-add, якщо користувач його окремо ввімкнув, створює запис на паузі й проходить ту саму валідацію й дедуплікацію. Smart collections є фільтрами локальної БД, а не дублюванням торрентів у `session`. ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html)) ([Core API](https://libtorrent.org/reference-Core.html)) ([Settings API](https://libtorrent.org/reference-Settings.html))

Для особистої збірки закріпити тег/commit libtorrent і версії Boost/TLS залежностей. Офіційна збірка підтримує CMake та Boost.Build; CMake вимагає C++17, а конфігурація та `TORRENT_*`/`BOOST_*` визначення мають узгоджуватися між збіркою бібліотеки й застосунку. Зібрати архітектуру власного Mac; кожна динамічна залежність повинна відповідати їй і розміщуватися в належному місці `.app`. Універсальний binary потрібен лише за потреби підтримувати обидві архітектури. Статичне лінкування — кандидат для першого spike, але вибір зробити після реального збирання, аналізу ліцензій і розміру артефакта. Не вимагати Homebrew у кінцевого користувача. ([Libtorrent building](https://libtorrent.org/building.html)) ([CMake: macOS architectures](https://cmake.org/cmake/help/latest/variable/CMAKE_OSX_ARCHITECTURES.html)) ([Apple: bundle content](https://developer.apple.com/documentation/bundleresources/placing-content-in-a-bundle))

Особисту збірку спершу перевіряємо локально на цільовому Mac. Для майбутньої публічної дистрибуції потрібен окремий конвеєр: Developer ID, Hardened Runtime, захищений timestamp, підпис вкладеного коду від внутрішніх бібліотек назовні, notarization і stapling. App Sandbox **не випливає автоматично** з `.dmg`/Developer ID; його вмикання — окреме рішення. Якщо ввімкнути, треба перевірити entitlement для вхідної й вихідної мережі, доступ до папки завантаження й папки спостереження та довгоживучі security-scoped bookmarks. Особисту збірку перевіряти на цільовому Mac без залежності від локальних Boost/OpenSSL; для публічної дистрибуції перевіряти всі підтримувані архітектури, з `codesign --verify`, `spctl`, notarization log і `stapler validate`, а також реальним імпортом, завантаженням, роздачею й відновленням. ([Apple: notarization](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution)) ([Apple: Hardened Runtime](https://developer.apple.com/documentation/security/hardened-runtime)) ([Apple: App Sandbox](https://developer.apple.com/documentation/security/app-sandbox))

Ліцензійні умови libtorrent та залежностей залишаються вимогою збірки й розповсюдження стороннього коду. Бінарний пакет має зберігати copyright, умови й disclaimer. `LICENSE` містить також тексти для включених фрагментів коду; Boost, OpenSSL/GnuTLS і фактично зібрані submodules потребують **окремого аудитного списку ліцензій**. У .app/.dmg слід дати `Third-Party Notices` та екран «Ліцензії». Це технічний checklist, не юридичний висновок: остаточний пакет notice треба перевірити за фактичним lockfile і linked objects. ([Libtorrent LICENSE](https://github.com/arvidn/libtorrent/blob/RC_2_1/LICENSE)) ([Libtorrent CMakeLists](https://github.com/arvidn/libtorrent/blob/RC_2_1/CMakeLists.txt))

## Прототип має довести ризикові сценарії до розробки всього інтерфейсу

Порядок реалізації для особистого використання. **Перший spike:** обрати tag 2.0.x або 2.1.x за компіляцією на власному Mac, зафіксувати SHA, зібрати його архітектуру, зробити мінімальний міст, додати тестовий `.torrent` і magnet, отримати alerts, виконати pause/resume, пріоритет файла, зберегти/відновити resume та вилучити запис зі збереженням payload. Окремо довести глобальне припинення віддачі при активному завантаженні. **Другий етап:** локальна БД, стабільний UUID, агреговані snapshots, імпорт-діалоги, коректні статуси, алерти й налаштування. **Третій:** bulk actions, пошук, фільтри, watch folder, правила й колекції. Перевірка інших архітектур і notarization потрібна за окремої потреби розповсюджувати збірку. ([Libtorrent building](https://libtorrent.org/building.html)) ([Session API](https://libtorrent.org/reference-Session.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

| Матриця перевірки | Що має бути доведено |
|---|---|
| Формати | v1, v2, hybrid; `.torrent`, magnet v1/v2, private torrent, multi-file, pad і zero-size файли. |
| Файли | Вибір/пропуск, boundary piece між файлами, зміна пріоритету під час transfer, progress і статус після пропуску всіх файлів. |
| Стан | Перезапуск під час download/seed, crash під час запису resume, зіпсований resume, переміщений або змінений payload, недоступна папка. |
| Мережа | Немає інтернету, недоступний tracker, DHT-only torrent, зміна Wi-Fi, сон/пробудження Mac, зайнятий listen port, proxy/VPN якщо з’являться. |
| Помилки | Заповнений диск, permission denied, invalid metainfo, нестача peers для magnet, alerts overflow, відмова видалити файли. |
| Продукт | Кілька торрентів додаються без штучного ліміту; watch folder проходить ту саму перевірку метаданих і дублікатів; auto-add створює запис на паузі; bulk delete лишає файли без окремого вибору. |
| Масштаб | Виміряти час запуску, RAM, CPU, alerts/sec, UI latency, disk I/O і батарею для бібліотек 100/1000 торрентів та великих file lists. |

Важливі рішення, які libtorrent **не прийме за нас**, треба зафіксувати до production-коду: мінімальна macOS; тег 2.0.x чи 2.1.x і схема оновлення; C ABI чи Objective-C++ bridge; App Sandbox; точні межі метаданих і політика symlink; термін локального зберігання resume; UX magnet metadata-only та «пропуску файла»; canonical key v1/v2/hybrid для локальної дедуплікації; транзакційне збереження тегів і колекцій; локалізація помилок; потреба і перевірений обсяг proxy/VPN функцій. Показники продуктивності й частоту snapshot не слід оголошувати наперед — їх задасть spike на найслабшому підтримуваному Mac.

## Висновок

Libtorrent скорочує обсяг роботи з BitTorrent-протоколом, але переносить головний інженерний ризик у **межу між асинхронним C++ рушієм і довговічним локальним станом macOS-застосунку**. Для особистої версії достатньо одного ізольованого engine, власної БД, alerts як джерела змін і локального fast-resume. Колекції, правила та папка спостереження працюють поверх цієї бібліотеки без серверного шару.

### Короткий словник

**Пір (peer)** — учасник swarm, який завантажує та/або віддає блоки. **Сід (seed)** — пір, який має весь потрібний набір даних. **Tracker** — сервіс пошуку пірів. **DHT** — розподілений пошук пірів без центрального tracker. **PEX** — обмін адресами пірів між учасниками. **LSD** — пошук пірів у локальній мережі. **Piece** — частина торрент-даних, цілісність якої перевіряється хешем; **block** — менша одиниця запиту до піра. **Info-hash** — хеш `info`-метаданих, ідентифікатор торрента у swarm. **Fast-resume** — локальний стан для швидкого продовження після перезапуску, не спосіб перенесення payload між Mac. ([BEP 3](https://www.bittorrent.org/beps/bep_0003.html)) ([Libtorrent manual](https://libtorrent.org/manual-ref.html))

### Основні першоджерела

| Тема | Джерело |
|---|---|
| Архітектура, життєвий цикл, приклади | [Manual](https://libtorrent.org/manual-ref.html), [Tutorial](https://libtorrent.org/tutorial-ref.html), [Internals](https://libtorrent.org/hacking.html) |
| Основні API | [Session](https://libtorrent.org/reference-Session.html), [Torrent handle](https://libtorrent.org/reference-Torrent_Handle.html), [Torrent status](https://libtorrent.org/reference-Torrent_Status.html), [Alerts](https://libtorrent.org/reference-Alerts.html) |
| Метадані й файли | [Core](https://libtorrent.org/reference-Core.html), [Torrent info](https://libtorrent.org/reference-Torrent_Info.html), [Storage](https://libtorrent.org/reference-Storage.html), [Resume data](https://libtorrent.org/reference-Resume_Data.html) |
| Протокол і версії | [BEP 3](https://www.bittorrent.org/beps/bep_0003.html), [BEP 52](https://bittorrent.org/beps/bep_0052.html), [Upgrade to 2.0](https://libtorrent.org/upgrade_to_2.0-ref.html) |
| Збірка й реліз | [Building](https://libtorrent.org/building.html), [Releases](https://github.com/arvidn/libtorrent/releases), [LICENSE](https://github.com/arvidn/libtorrent/blob/RC_2_1/LICENSE), [Apple notarization](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution) |
