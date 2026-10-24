# OSTOM

[en](README.md) | [es](README.es.md) | [fr](README.fr.md) | [de](README.de.md) | [nl](README.nl.md) | [ja](README.ja.md) | [ru](README.ru.md) | [he](README.he.md)

> Parfois, Outlook conserve trop de messages.

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)
[![.NET](https://img.shields.io/badge/.NET-8.0-512BD4?logo=dotnet&logoColor=white)](https://dotnet.microsoft.com/download/dotnet/8.0)

OSTOM est un utilitaire en lecture seule qui récupère les messages lisibles
depuis les fichiers PST/OST d’Outlook et les exporte aux formats mbox et EML.

OSTOM lit les fichiers de données Outlook sans les modifier. Lorsqu’un fichier
OST/PST n’est que partiellement lisible, il copie les messages et les pièces
jointes récupérables dans une arborescence mbox compatible avec Thunderbird,
signale les parties endommagées et laisse le fichier source intact.

En bref, OSTOM est un outil local de récupération avec un rapport clair. Il ne
répare pas et ne reconstruit pas les fichiers PST/OST. Les messages et les
pièces jointes récupérés sont enregistrés dans des formats d’export ouverts.

<p align="center">
  <img src="docs/accountant-recovered-mail.png"
       alt="Une comptable se réjouit du courrier récupéré"
       width="900">
</p>

- l’export indexé normal conserve les dossiers et les messages lisibles ;
- une procédure de récupération indépendante peut retrouver d’autres messages
  lorsque l’index est endommagé ;
- les messages et les pièces jointes sont écrits au fur et à mesure de leur
  lecture, de sorte qu’un élément endommagé ne fait pas perdre le reste des
  données lisibles ;
- le rapport et le manifeste distinguent les résultats exportés ou partiels,
  les échecs et les doublons.

## Démarrage rapide

Travaillez sur une copie du fichier et conservez l’original intact.

L’idée principale est simple : ouvrez le résultat d’OSTOM dans Thunderbird,
puis organisez les messages dans des dossiers IMAP. Commencez par charger
l’export dans le dossier local « Dossiers locaux » de Thunderbird et vérifiez-le.
Dans le profil, ce dossier apparaît sur le disque sous le nom `Local Folders`.
Créez ensuite les dossiers IMAP nécessaires et copiez-y ou déplacez-y les
messages. OSTOM récupère les données lisibles ; Thunderbird les affiche et peut
les charger sur le serveur. OSTOM ne se connecte pas à IMAP et ne modifie jamais
le PST/OST source.

Vérifiez la structure et enregistrez un rapport JSON :

```powershell
.\ostom.exe -json -output-dir E:\recovered source-copy.ost
```

Exportez les messages lisibles :

```powershell
.\ostom.exe -export-dir E:\recovered source-copy.ost
```

Si le fichier est endommagé ou si la voie normale ne suffit pas, forcez la
procédure de récupération séparée :

```powershell
.\ostom.exe -recovery-scan -export-dir E:\recovered source-copy.ost
```

Fermez Thunderbird, copiez le dossier `Local Folders` créé dans son profil et
vérifiez les messages et les pièces jointes récupérés avant de les utiliser.
Consultez le [guide d’importation dans Thunderbird](docs/THUNDERBIRD.fr.md) pour
le flux complet : d’abord en local, puis dans IMAP.

## Options

```text
ostom [-json] [-check|-analyze] [-progress-json] [-eml]
      [-output-dir DIR] [-export-dir DIR]
      [-recovery-scan] [-native-only] [-quiet] file.pst|file.ost
```

- `-check` — vérification rapide de la structure sans lire le contenu des
  messages ;
- `-analyze` — analyse limitée du contenu sans export ;
- `-export-dir DIR` — destination du courrier récupéré. L’export normal est
  écrit sous `Local Folders` ;
- `-output-dir DIR` — destination séparée pour `ostom-report.json` et, lors
  d’une récupération, pour `recovery-manifest.json`/`.csv`. Si elle est omise,
  ces fichiers sont écrits dans `-export-dir` ;
- `-recovery-scan` — lance une procédure de récupération supplémentaire pour
  les fichiers endommagés ;
- `-native-only` — lance uniquement la procédure de récupération supplémentaire ;
- `-eml` — écrit un fichier `.eml` séparé pour chaque message récupéré, dans
  chaque chemin d’export sélectionné ;
- `-json` — écrit le rapport final au format JSON ;
- `-progress-json` — écrit les événements de progression dans un format lisible
  par machine sur stderr ;
- `-quiet` — masque le texte de progression.

`-recovery-scan` et `-native-only` nécessitent `-export-dir`.

## À quoi s’attendre

OSTOM indique ce qui a pu être lu dans ce fichier précis. Une exécution réussie
ne prouve pas que le fichier ne contient aucune donnée illisible. Certains
messages récupérés peuvent ne pas retrouver leur dossier d’origine. Un fichier
mbox vide peut simplement représenter un dossier source vide. Consultez le
rapport avant de supprimer la source ou sa copie de sauvegarde.

L’interface graphique Windows est une interface simple pour le même CLI. Gardez
`ostom-gui.exe` et le `ostom.exe` correspondant dans le même dossier. Elle
nécessite le .NET 8 Desktop Runtime pour x64.

## Compilation et documentation

La version 1.24 ou ultérieure de Go est requise :

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [Détails du format et de la récupération](docs/FORMAT.md) (anglais)
- [Importation dans Thunderbird](docs/THUNDERBIRD.fr.md)
- [Plan de développement](docs/TODO.md) (anglais)
- [Avis concernant les composants tiers](docs/THIRD_PARTY_NOTICES.md) (anglais)

OSTOM est indépendant de Microsoft et n’envoie pas les données source sur le
réseau. Les binaires publiés ne sont pas encore signés ; vérifiez les fichiers
téléchargés avec les sommes de contrôle SHA-256 publiées.
