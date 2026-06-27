#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 5 ]; then
  echo "usage: $0 <tag> <darwin-amd64-sha256> <darwin-arm64-sha256> <linux-amd64-sha256> <linux-arm64-sha256>" >&2
  exit 2
fi

tag="$1"
darwin_amd64_sha="$2"
darwin_arm64_sha="$3"
linux_amd64_sha="$4"
linux_arm64_sha="$5"
version="${tag#v}"

cat <<FORMULA
class Pgferry < Formula
  desc "Migrate MySQL, MariaDB, SQLite, or MSSQL databases to PostgreSQL"
  homepage "https://www.pgferry.com"
  version "${version}"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Limetric/pgferry/releases/download/${tag}/pgferry-darwin-arm64"
      sha256 "${darwin_arm64_sha}"
    elsif Hardware::CPU.intel?
      url "https://github.com/Limetric/pgferry/releases/download/${tag}/pgferry-darwin-amd64"
      sha256 "${darwin_amd64_sha}"
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/Limetric/pgferry/releases/download/${tag}/pgferry-linux-arm64"
      sha256 "${linux_arm64_sha}"
    elsif Hardware::CPU.intel?
      url "https://github.com/Limetric/pgferry/releases/download/${tag}/pgferry-linux-amd64"
      sha256 "${linux_amd64_sha}"
    end
  end

  def install
    binary = Dir["pgferry-*"].first
    chmod 0755, binary
    bin.install binary => "pgferry"
  end

  test do
    system "#{bin}/pgferry", "version"
  end
end
FORMULA
