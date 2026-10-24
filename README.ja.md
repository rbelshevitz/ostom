# OSTOM

[en](README.md) | [es](README.es.md) | [fr](README.fr.md) | [de](README.de.md) | [nl](README.nl.md) | [ja](README.ja.md) | [ru](README.ru.md) | [he](README.he.md)

> Outlook にメールがたまりすぎることがあります。

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)
[![.NET](https://img.shields.io/badge/.NET-8.0-512BD4?logo=dotnet&logoColor=white)](https://dotnet.microsoft.com/download/dotnet/8.0)

OSTOM は、Outlook の PST/OST データファイルから読み取り可能なメールを復元し、
mbox および EML 形式へエクスポートする読み取り専用のツールです。

OSTOM は Outlook のデータファイルを変更せずに読み取ります。OST/PST ファイルを
一部しか読み取れない場合でも、復元できたメールと添付ファイルを Thunderbird 互換の
mbox フォルダー構造へコピーし、破損した部分をレポートに記録します。元のファイルは
変更されません。

つまり OSTOM は、結果を正直に報告するローカル復元ツールです。PST/OST ファイルを
修復したり、作り直したりするものではありません。復元したメールと添付ファイルは、
オープンなエクスポート形式で保存します。

<p align="center">
  <img src="docs/recovered-mail.png"
       alt="復元したメールを見て喜ぶ経理担当者"
       width="900">
</p>

- 通常のインデックス付きエクスポートでは、読み取れたフォルダーとメールを保持します。
- インデックスが破損している場合は、別の復元経路によって追加のメールが見つかる
  ことがあります。
- メールと添付ファイルは読み取りながら書き込むため、1 件の破損した項目によって
  残りの読み取り可能なデータが失われることはありません。
- レポートとマニフェストでは、エクスポート済み、部分的、失敗、重複の結果を区別します。

## クイックスタート

ファイルのコピーを使い、元のファイルはそのまま保存してください。

基本的な流れは簡単です。OSTOM の結果を Thunderbird で開き、メールを IMAP の
フォルダーに整理します。まず Thunderbird の「ローカルフォルダー」にエクスポートを
読み込み、内容を確認してください。プロファイル内のディスク上のフォルダー名は通常
`Local Folders` です。その後、必要な IMAP フォルダーを作成し、
メールをコピーまたは移動します。OSTOM は読み取り可能なデータを復元し、Thunderbird
はそれを表示してサーバーへアップロードできます。OSTOM 自身は IMAP に接続せず、元の
PST/OST も変更しません。

構造を確認して JSON レポートを保存します。

```powershell
.\ostom.exe -json -output-dir E:\recovered source-copy.ost
```

読み取り可能なメールをエクスポートします。

```powershell
.\ostom.exe -export-dir E:\recovered source-copy.ost
```

ファイルが破損している場合や通常の経路で十分な結果が得られない場合は、別の復元処理を
明示的に実行します。

```powershell
.\ostom.exe -recovery-scan -export-dir E:\recovered source-copy.ost
```

Thunderbird を終了し、作成された `Local Folders` フォルダーをプロファイルへコピーして、
復元されたメールと添付ファイルを確認してください。ローカルで確認してから IMAP へ
アップロードする手順は、[Thunderbird へのインポートガイド](docs/THUNDERBIRD.ja.md)
にまとめています。

## オプション

```text
ostom [-json] [-check|-analyze] [-progress-json] [-eml]
      [-output-dir DIR] [-export-dir DIR]
      [-recovery-scan] [-native-only] [-quiet] file.pst|file.ost
```

- `-check` — メール本文を読まずに構造をすばやく確認します。
- `-analyze` — エクスポートせず、内容を限定的に分析します。
- `-export-dir DIR` — 復元したメールの出力先です。通常のエクスポートは
  `Local Folders` の下に作成されます。
- `-output-dir DIR` — `ostom-report.json` と、復元を実行した場合の
  `recovery-manifest.json`/`.csv` を別の場所に保存します。省略した場合は
  `-export-dir` に保存されます。
- `-recovery-scan` — 破損したファイルに対して追加の復元処理を実行します。
- `-native-only` — 追加の復元処理だけを実行します。
- `-eml` — 復元した各メールを、選択した各エクスポート先に個別の `.eml` ファイルとして
  保存します。
- `-json` — 最終レポートを JSON 形式で出力します。
- `-progress-json` — 機械で読み取れる進捗イベントを stderr に出力します。
- `-quiet` — 進捗テキストを表示しません。

`-recovery-scan` と `-native-only` には `-export-dir` が必要です。

## 実行結果について

OSTOM は、指定したそのファイルから読み取れた内容を報告します。処理が成功しても、
読み取れないデータが存在しないことを意味するわけではありません。復元したメールの
一部は、元のフォルダーに戻せない場合があります。空の mbox ファイルは、元の空の
フォルダーを表しているだけかもしれません。元のファイルやバックアップを削除する前に、
レポートを確認してください。

Windows GUI は同じ CLI 用のシンプルな画面です。`ostom-gui.exe` と対応する `ostom.exe`
を同じフォルダーに置いてください。GUI には x64 版の .NET 8 Desktop Runtime が必要です。

## ビルドとドキュメント

Go 1.24 以降が必要です。

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [形式と復元の詳細](docs/FORMAT.md)（英語）
- [Thunderbird へのインポート](docs/THUNDERBIRD.ja.md)
- [開発計画](docs/TODO.md)（英語）
- [サードパーティコンポーネントのライセンス通知](docs/THIRD_PARTY_NOTICES.md)（英語）

OSTOM は Microsoft とは独立したツールであり、元のデータをネットワーク経由で送信しません。
リリースバイナリには現在署名がありません。ダウンロードしたアーカイブは、公開されている
SHA-256 の値で確認してください。
