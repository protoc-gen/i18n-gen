# i18n-proto

Generate i18n files from proto files.

## Install

```bash
go install github.com/protoc-gen/i18n-gen@latest
```

- Install of local

```bash
go install .
```

## Usage

```bash
i18n-gen -O ./i18n/ -P ./proto/api/**.proto -L en,ja,zh -suffix Error
```

## Options

- `-O`: Output directory
- `-P`: Proto file pattern
- `-L`: Languages
- `-prefix`: Prefix of enum name
- `-suffix`: Suffix of enum name
