#!/usr/bin/env python3
"""Restaura o ZIP original a partir de fragmentos temporários GitHub.
Executa apenas na preparação inicial; binários originais não são alterados.
"""
from pathlib import Path
import base64
import hashlib
import io
import zipfile

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / "assets/GameList-main.zip"
EXPECTED_SHA256 = "fdc941b5210152e2ca30ed1215a19082085a0866129e83f9497f090e3b2aff48"


def main():
    if TARGET.exists():
        digest = hashlib.sha256(TARGET.read_bytes()).hexdigest()
        if digest != EXPECTED_SHA256:
            raise SystemExit("Arquivo GameList-main.zip não corresponde ao catálogo validado")
        print("Catálogo original verificado.")
        return

    files = sorted((ROOT / "assets/.chunks").glob("part-*.txt"))
    if not files:
        raise SystemExit("Catálogo não encontrado; faltam os fragmentos temporários")
    encoded = "".join(f.read_text(encoding="ascii") for f in files)
    content = base64.b64decode(encoded, validate=True)
    if hashlib.sha256(content).hexdigest() != EXPECTED_SHA256:
        raise SystemExit("Catálogo inválido: hash SHA-256 não corresponde ao original")
    with zipfile.ZipFile(io.BytesIO(content)) as archive:
        names = {Path(name).name for name in archive.namelist()}
        if not {"games.json", "dlc.json"}.issubset(names) or archive.testzip():
            raise SystemExit("Conteúdo ZIP inválido")
    TARGET.parent.mkdir(parents=True, exist_ok=True)
    TARGET.write_bytes(content)
    print(f"Catálogo restaurado e verificado: {len(content)} bytes")


if __name__ == "__main__":
    main()
