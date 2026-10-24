# Charger la sortie d’OSTOM dans Thunderbird

[en](THUNDERBIRD.md) | [es](THUNDERBIRD.es.md) | [fr](THUNDERBIRD.fr.md) | [de](THUNDERBIRD.de.md) | [ru](THUNDERBIRD.ru.md) | [he](THUNDERBIRD.he.md)

## L’idée principale : d’abord en local, puis dans IMAP

OSTOM n’envoie pas les messages récupérés vers un serveur. Il produit une
arborescence que Thunderbird peut ouvrir :

```text
PST/OST → OSTOM → Dossiers locaux → dossiers du compte IMAP
```

Dans l’interface française, cette zone s’appelle « Dossiers locaux ». Dans le
profil, le dossier sur le disque porte normalement le nom `Local Folders`.

Après avoir vérifié le résultat local, utilisez Thunderbird pour envoyer les
messages vers IMAP. Créez les dossiers nécessaires sur le serveur, puis copiez
ou déplacez-y les messages depuis `Local Folders`. Les sous-dossiers se gèrent
de la même manière. Vous pouvez ainsi réorganiser les messages récupérés même
si OSTOM n’a pas pu identifier le dossier d’origine de chaque message.

Vérifiez d’abord la copie locale. Si vous commencez directement dans IMAP, un
déplacement erroné peut être synchronisé avec le serveur, se heurter aux quotas
ou rendre le résultat plus difficile à retrouver en cas d’erreur de
synchronisation.

OSTOM exporte les dossiers locaux de Thunderbird dans la structure mbox standard :

```text
output/
├── Local Folders/
│   ├── Local Sent
│   ├── Local Sent.sbd/
│   │   ├── 2022
│   │   └── 2023
│   └── Archive
└── report.json
```

Chaque boîte aux lettres est un fichier sans extension. Un dossier qui contient
des sous-dossiers possède les deux éléments suivants :

```text
FolderName       # boîte mbox contenant les messages
FolderName.sbd/  # sous-dossiers
```

Thunderbird crée les fichiers d’index `.msf`. OSTOM ne doit pas les créer : des
index anciens peuvent masquer des messages ou afficher un nombre incorrect.

## Importation recommandée

1. Créez ou ouvrez un profil Thunderbird.
2. Lancez Thunderbird une fois pour faire apparaître « Dossiers locaux ».
3. Fermez complètement Thunderbird.
4. Sauvegardez le profil Thunderbird.
5. Copiez le contenu du dossier `Local Folders` exporté par OSTOM dans
   `Mail/Local Folders/` du profil.
6. Relancez Thunderbird.
7. Attendez que Thunderbird reconstruise les index des dossiers.

Le profil se trouve généralement ici :

```text
%APPDATA%\Thunderbird\Profiles\<profile>\
```

Pour les importations volumineuses ou incertaines, utilisez un nouveau profil
ou l’extension ImportExportTools NG. Importez d’abord dans « Dossiers locaux »,
et non directement dans un dossier IMAP actif. Vous éviterez ainsi d’envoyer
sur le serveur des messages récupérés qui n’ont pas encore été vérifiés.

## Envoyer les messages vérifiés vers IMAP

1. Ajoutez ou ouvrez le compte IMAP cible dans Thunderbird.
2. Créez-y les dossiers et sous-dossiers nécessaires.
3. Sélectionnez les messages vérifiés ou un dossier sous « Dossiers locaux ».
4. Faites-les glisser vers le dossier IMAP ou utilisez les commandes de copie et
   de déplacement de Thunderbird.
5. Attendez la fin de la synchronisation et vérifiez plusieurs messages, pièces
   jointes et sous-dossiers dans le webmail ou un autre client.

Pour le premier transfert, copiez plutôt que de déplacer afin de conserver
l’export local comme copie de sauvegarde. Tenez compte des quotas IMAP, des
limites de taille des pièces jointes et des règles du serveur concernant les
noms de dossiers.

## Règles de sécurité

- Copiez le résultat ; ne déplacez pas l’unique export.
- Conservez `report.json` à côté de l’export.
- Ne copiez pas les fichiers `.msf` d’un ancien profil.
- N’importez pas dans IMAP avant d’avoir vérifié l’export local.
- Conservez l’OST original et le rapport OSTOM.

Le stockage local de Thunderbird utilise les messages RFC 5322 d’origine dans
des fichiers mbox et une base `.msf` distincte pour chaque dossier. Le fichier
mbox contient les données ; `.msf` est un index qui peut être reconstruit.
