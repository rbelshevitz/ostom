# Cargar el resultado de OSTOM en Thunderbird

[en](THUNDERBIRD.md) | [es](THUNDERBIRD.es.md) | [fr](THUNDERBIRD.fr.md) | [de](THUNDERBIRD.de.md) | [nl](THUNDERBIRD.nl.md) | [ja](THUNDERBIRD.ja.md) | [ru](THUNDERBIRD.ru.md) | [he](THUNDERBIRD.he.md)

## La idea principal: primero local, después IMAP

OSTOM no sube el correo recuperado a ningún servidor. Produce una estructura de
carpetas que Thunderbird puede abrir:

```text
PST/OST → OSTOM → Carpetas locales → carpetas de la cuenta IMAP
```

En la interfaz en español, el área local aparece como «Carpetas locales». En
el perfil, la carpeta del disco normalmente se llama `Local Folders`.

Después de revisar el resultado local, use Thunderbird para subir los mensajes
a IMAP. Cree las carpetas necesarias en el servidor y copie o mueva allí los
mensajes desde `Local Folders`. Las subcarpetas funcionan igual. Así puede
volver a organizar el correo aunque OSTOM no haya podido identificar la carpeta
original de todos los mensajes.

Compruebe primero la copia local. Si empieza directamente en IMAP, un traslado
equivocado puede sincronizarse con el servidor, puede encontrarse con límites de
cuota o puede ser más difícil localizar el resultado durante un error de
sincronización.

OSTOM exporta las carpetas locales de Thunderbird en la estructura mbox estándar:

```text
output/
├── Local Folders/
│   ├── Local Sent
│   ├── Local Sent.sbd/
│   │   ├── 2022
│   │   └── 2023
│   └── Archive
└── ostom-report.json
```

Cada buzón es un archivo sin extensión. Una carpeta con subcarpetas tiene ambos:

```text
FolderName       # almacén mbox de mensajes
FolderName.sbd/  # subcarpetas
```

Thunderbird crea los archivos de índice `.msf`. OSTOM no debe crearlos: los
índices antiguos pueden ocultar mensajes o mostrar cantidades incorrectas.

## Importación recomendada

1. Cree o abra un perfil de Thunderbird.
2. Inicie Thunderbird una vez para que aparezca «Carpetas locales».
3. Cierre Thunderbird por completo.
4. Haga una copia de seguridad del perfil.
5. Copie el contenido de la carpeta `Local Folders` exportada por OSTOM en
   `Mail/Local Folders/` dentro del perfil.
6. Inicie Thunderbird.
7. Espere a que Thunderbird reconstruya los índices de las carpetas.

El perfil suele encontrarse en:

```text
%APPDATA%\Thunderbird\Profiles\<profile>\
```

Para importaciones grandes o dudosas, use un perfil nuevo o la extensión
ImportExportTools NG. Importe primero en «Carpetas locales», no directamente en
una carpeta IMAP activa. Así evita subir al servidor mensajes recuperados que
todavía no ha verificado.

## Subir mensajes verificados a IMAP

1. Añada o abra la cuenta IMAP de destino en Thunderbird.
2. Cree allí las carpetas y subcarpetas de destino.
3. Seleccione los mensajes verificados o una carpeta bajo «Carpetas locales».
4. Arrástrelos a la carpeta IMAP o use los comandos de copiar/mover de
   Thunderbird.
5. Espere a que termine la sincronización y compruebe varios mensajes, archivos
   adjuntos y subcarpetas mediante el webmail u otro cliente.

Para la primera pasada, copie en vez de mover para conservar la exportación
local como copia de seguridad. Tenga en cuenta las cuotas de IMAP, los límites
de tamaño de los archivos adjuntos y las reglas del servidor para los nombres
de las carpetas.

## Reglas de seguridad

- Copie el resultado; no mueva la única exportación.
- Conserve `ostom-report.json` junto a la exportación.
- No copie archivos `.msf` de un perfil antiguo.
- No importe en IMAP hasta haber verificado la exportación local.
- Conserve el OST original y el informe de OSTOM.

El almacenamiento local de Thunderbird usa mensajes RFC 5322 sin modificar en
archivos mbox y una base de datos `.msf` separada para cada carpeta. El archivo
mbox contiene los datos; `.msf` es un índice que se puede reconstruir.
