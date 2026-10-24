# OSTOM

[en](README.md) | [es](README.es.md) | [fr](README.fr.md) | [de](README.de.md) | [ru](README.ru.md) | [he](README.he.md)

> A veces, todo el correo en Outlook es una carga pesada.

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)
[![.NET](https://img.shields.io/badge/.NET-8.0-512BD4?logo=dotnet&logoColor=white)](https://dotnet.microsoft.com/download/dotnet/8.0)

OSTOM es una utilidad de solo lectura para recuperar mensajes legibles de
archivos de datos PST/OST de Outlook y exportarlos en formatos mbox y EML.

OSTOM lee los archivos de datos de Outlook sin modificarlos. Cuando un archivo
OST/PST solo se puede leer parcialmente, copia los mensajes y archivos adjuntos
que puede recuperar a una estructura de carpetas mbox compatible con Thunderbird,
informa de las partes dañadas y deja intacto el archivo de origen.

En resumen, OSTOM es una herramienta local de recuperación con un informe claro.
No repara ni reconstruye archivos PST/OST. Guarda los mensajes y archivos
adjuntos recuperados en formatos de exportación abiertos.

<p align="center">
  <img src="docs/accountant-recovered-mail.png"
       alt="Una contable celebra el correo recuperado"
       width="900">
</p>

- la exportación indexada normal conserva las carpetas y los mensajes legibles;
- una ruta de recuperación independiente puede encontrar mensajes adicionales
  cuando el índice está dañado;
- los mensajes y archivos adjuntos se escriben a medida que se leen, de modo
  que un elemento dañado no descarta el resto de los datos legibles;
- el informe y el manifiesto distinguen entre resultados exportados, parciales,
  fallidos y duplicados.

## Inicio rápido

Trabaje con una copia del archivo y conserve el original sin modificar.

La idea principal es sencilla: abra el resultado de OSTOM en Thunderbird y
organice allí los mensajes en carpetas IMAP. Primero cargue la exportación en
la carpeta local `Local Folders` de Thunderbird y compruébela. Después cree las
carpetas IMAP necesarias y copie o mueva allí los mensajes. OSTOM recupera los
datos legibles; Thunderbird los muestra y puede cargarlos en el servidor.
OSTOM no se conecta a IMAP ni modifica el PST/OST de origen.

Compruebe la estructura y guarde un informe JSON:

```powershell
.\ostom.exe -json -output-dir E:\recovered source-copy.ost
```

Exporte los mensajes legibles:

```powershell
.\ostom.exe -export-dir E:\recovered source-copy.ost
```

Si el archivo está dañado o la ruta normal no es suficiente, fuerce el proceso
de recuperación independiente:

```powershell
.\ostom.exe -recovery-scan -export-dir E:\recovered source-copy.ost
```

Cierre Thunderbird, copie la carpeta `Local Folders` resultante a su perfil y
compruebe los mensajes y archivos adjuntos recuperados antes de utilizarlos.
Consulte la [guía de importación en Thunderbird](docs/THUNDERBIRD.md) para el
flujo completo: primero en local y después en IMAP.

## Opciones

```text
ostom [-json] [-check|-analyze] [-progress-json] [-eml]
      [-output-dir DIR] [-export-dir DIR]
      [-recovery-scan] [-native-only] [-quiet] file.pst|file.ost
```

- `-check` — comprobación rápida de la estructura sin leer el contenido de los
  mensajes.
- `-analyze` — análisis limitado del contenido sin exportarlo.
- `-export-dir DIR` — destino del correo recuperado. La exportación normal se
  escribe dentro de `Local Folders`.
- `-output-dir DIR` — destino separado para `ostom-report.json` y, cuando se
  ejecuta el proceso de recuperación, `recovery-manifest.json`/`.csv`. Si se omite, estos
  archivos se escriben en `-export-dir`.
- `-recovery-scan` — ejecuta un proceso de recuperación adicional para archivos
  dañados.
- `-native-only` — ejecuta solo el proceso de recuperación adicional.
- `-eml` — escribe un archivo `.eml` separado por cada mensaje recuperado en
  cada ruta de exportación seleccionada.
- `-json` — escribe el informe final en JSON.
- `-progress-json` — escribe eventos de progreso en un formato legible por
  máquina en stderr.
- `-quiet` — oculta el texto del progreso.

`-recovery-scan` y `-native-only` requieren `-export-dir`.

## Qué esperar

OSTOM informa de lo que se pudo leer de ese archivo concreto. Una ejecución
correcta no demuestra que no haya datos ilegibles. Algunos mensajes recuperados
pueden no conservar su carpeta original. Un archivo mbox vacío puede representar
simplemente una carpeta de origen vacía. Revise el informe antes de eliminar el
archivo de origen o su copia de seguridad.

La interfaz gráfica de Windows es una interfaz sencilla para el mismo CLI.
Mantenga `ostom-gui.exe` junto con el `ostom.exe` correspondiente en la misma carpeta.
La interfaz requiere .NET 8 Desktop Runtime de x64.

## Compilación y documentación

Se requiere Go 1.24 o posterior:

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [Detalles del formato y la recuperación](docs/FORMAT.md) (inglés)
- [Importación en Thunderbird](docs/THUNDERBIRD.md) (inglés)
- [Plan de desarrollo](docs/TODO.md) (inglés)
- [Avisos de terceros](docs/THIRD_PARTY_NOTICES.md) (inglés)

OSTOM es independiente de Microsoft y no envía los datos de origen por la red.
Los binarios de lanzamiento todavía no están firmados; verifique los archivos
descargados con las sumas SHA-256 publicadas.
