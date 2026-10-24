# OSTOM

[en](README.md) | [es](README.es.md) | [fr](README.fr.md) | [de](README.de.md) | [ru](README.ru.md) | [he](README.he.md)

> לפעמים כל הדואר ב-Outlook הוא משא כבד.

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)
[![.NET](https://img.shields.io/badge/.NET-8.0-512BD4?logo=dotnet&logoColor=white)](https://dotnet.microsoft.com/download/dotnet/8.0)

OSTOM הוא כלי לקריאה בלבד לשחזור הודעות דואר שניתן לקרוא מקובצי נתונים מסוג
PST/OST של Outlook ולייצואן לפורמטים mbox ו-EML.

OSTOM קורא את קובצי הנתונים של Outlook בלי לשנות אותם. כאשר קובץ נתונים מסוג
OST/PST ניתן לקריאה רק בחלקו, הוא מעתיק את ההודעות ואת הקבצים המצורפים שניתן
לשחזר למבנה תיקיות mbox התואם ל-Thunderbird, מדווח על החלקים שנפגעו ומשאיר את קובץ
המקור ללא שינוי.

בקיצור, OSTOM הוא כלי מקומי לשחזור עם דוח ברור. הוא לא מתקן ולא בונה מחדש
קובצי PST/OST. ההודעות והקבצים המצורפים נשמרים בפורמטי ייצוא פתוחים.

<p align="center">
  <img src="docs/accountant-recovered-mail.png"
       alt="רואת חשבון שמחה על הדואר ששוחזר"
       width="900">
</p>

- הייצוא הרגיל שומר את התיקיות ואת ההודעות שניתן לקרוא;
- מסלול שחזור נפרד יכול למצוא הודעות נוספות כאשר האינדקס פגום;
- הודעות וקבצים מצורפים נכתבים בזמן הקריאה, כך שפריט פגום אחד לא מוחק את
  שאר התוצאה;
- הדוח והמניפסט מבדילים בין תוצאות שיוצאו, תוצאות חלקיות, כישלונות וכפילויות.

## התחלה מהירה

עבדו על עותק של הקובץ ושמרו את המקור.

הרעיון המרכזי פשוט: פותחים את התוצאה של OSTOM ב-Thunderbird, ואז מסדרים את
ההודעות בתיקיות של חשבון IMAP. קודם טוענים את הייצוא לתיקייה המקומית
`Local Folders` של Thunderbird ובודקים אותו. אחר כך יוצרים את תיקיות ה-IMAP
הדרושות ומעתיקים או מעבירים אליהן את ההודעות. OSTOM משחזר את המידע שניתן
לקרוא; Thunderbird מציג
את ההודעות ויכול להעלות אותן לשרת. OSTOM עצמו לא מתחבר ל-IMAP ולא משנה את קובץ
המקור.

בדיקת המבנה ושמירת דוח JSON:

```powershell
.\ostom.exe -json -output-dir E:\recovered source-copy.ost
```

ייצוא הודעות שניתן לקרוא:

```powershell
.\ostom.exe -export-dir E:\recovered source-copy.ost
```

אם הקובץ פגום או שהמסלול הרגיל אינו מספיק, הפעילו את מסלול השחזור הנפרד:

```powershell
.\ostom.exe -recovery-scan -export-dir E:\recovered source-copy.ost
```

סגרו את Thunderbird, העתיקו את תיקיית `Local Folders` שנוצרה אל הפרופיל שלו
ובדקו את ההודעות ואת הקבצים המצורפים. לפרטים על התהליך המלא — קודם תיקיות
מקומיות ואחר כך IMAP — עיינו ב[מדריך הייבוא ל-Thunderbird](docs/THUNDERBIRD.he.md).

## אפשרויות

```text
ostom [-json] [-check|-analyze] [-progress-json] [-eml]
      [-output-dir DIR] [-export-dir DIR]
      [-recovery-scan] [-native-only] [-quiet] file.pst|file.ost
```

- `-check` — בדיקת מבנה מהירה בלי לקרוא את תוכן ההודעות.
- `-analyze` — ניתוח מוגבל של התוכן בלי לייצא אותו.
- `-export-dir DIR` — תיקיית היעד לדואר ששוחזר. הייצוא הרגיל נכתב תחת
  `Local Folders`.
- `-output-dir DIR` — תיקייה נפרדת עבור `ostom-report.json` ובעת שחזור גם עבור
  `recovery-manifest.json` ו-`recovery-manifest.csv`. אם לא מציינים אותה, הקבצים נכתבים אל
  `-export-dir`.
- `-recovery-scan` — הפעלת מסלול שחזור נוסף עבור קבצים פגומים.
- `-native-only` — הפעלת מסלול השחזור הנוסף בלבד.
- `-eml` — שמירת קובץ `.eml` נפרד עבור כל הודעה ששוחזרה, בכל נתיב ייצוא שנבחר.
- `-json` — כתיבת הדוח הסופי בפורמט JSON.
- `-progress-json` — כתיבת אירועי התקדמות הניתנים לקריאה במכונה אל stderr.
- `-quiet` — הסתרת טקסט ההתקדמות.

האפשרויות `-recovery-scan` ו-`-native-only` דורשות את `-export-dir`.

## למה לצפות

OSTOM מדווח מה ניתן היה לקרוא מהקובץ המסוים הזה. ריצה מוצלחת אינה מוכיחה
שאין בו מידע שאי אפשר לקרוא. חלק מההודעות המשוחזרות אולי לא יישארו בתיקייה
המקורית שלהן. קובץ mbox ריק יכול פשוט לייצג תיקיית מקור ריקה. בדקו את הדוח
לפני שמוחקים את המקור או את עותק הגיבוי שלו.

הממשק הגרפי של Windows הוא ממשק קטן לאותו CLI. שמרו את `ostom-gui.exe` ואת
`ostom.exe` המתאים באותה תיקייה. הממשק דורש את סביבת ההרצה .NET 8 Desktop
Runtime בגרסת x64.

## בנייה ותיעוד

נדרשת גרסת Go 1.24 ומעלה:

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [פרטי הפורמט והשחזור](docs/FORMAT.md) (אנגלית)
- [ייבוא ל-Thunderbird](docs/THUNDERBIRD.he.md)
- [תוכנית הפיתוח](docs/TODO.md) (אנגלית)
- [הודעות על רישיונות רכיבי צד שלישי](docs/THIRD_PARTY_NOTICES.md) (אנגלית)

OSTOM אינו קשור ל-Microsoft ואינו שולח את נתוני המקור דרך הרשת. קובצי ההפצה
אינם חתומים כרגע; אמתו את הארכיונים שהורדתם לפי ערכי ה-SHA-256 שפורסמו.
