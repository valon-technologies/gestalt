# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.16"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.16/gestalt-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "ea7fd9b54d7b56e849709ebe4787a4bcd11c685c32f2a1e2f7b9b2b6219b6c81"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.16/gestalt-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "f7344d91c8d41fb2fff8ff2fdd0aea658a37b73d412558e527a48e82cc2b372e"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.16/gestalt-linux-arm64-musl.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "13ba587b1b9fd30eabc56ae951b9061b120fad490642bb5434599832bac8888c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.16/gestalt-linux-x86_64-musl.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "c4460c2b2dfb893c786a7c2549d3fe16cdeda06a9be8f6d04d483e9132338b13"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
