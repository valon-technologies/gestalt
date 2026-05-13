# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1/gestalt-macos-arm64.tar.gz"
      sha256 "6c75c37477c616284816021cb98c1fc59c8c3fc40baf11ae1debc4f2057b01c4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1/gestalt-macos-x86_64.tar.gz"
      sha256 "075490c157132c3c8e81f218908ce690b524ed314ecd152c9254febf7992ec57"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1/gestalt-linux-arm64.tar.gz"
      sha256 "0629f1d284392f3026b0cc4217e7c0a54f756a2af8b8835ee090b72bff8cc2ff"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1/gestalt-linux-x86_64.tar.gz"
      sha256 "95583cb2dd8706b168982575b06cb80ee205e7afb857f7ffbf2606b9e586fd09"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
