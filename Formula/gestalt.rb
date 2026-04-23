# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.5"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.5/gestalt-macos-arm64.tar.gz"
      sha256 "35684afc14d41e103db449d711fcdac63b413cf7f106de09f5886c879ecef637"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.5/gestalt-macos-x86_64.tar.gz"
      sha256 "c6c677ea61d68f47b35a26e6ff92de7341d6053a56f871e0cc5ce26ec4ef31cc"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.5/gestalt-linux-arm64.tar.gz"
      sha256 "9f974add03e62503c84de4a9f4894a84de17f329b473639842a7951ce47d9f83"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.5/gestalt-linux-x86_64.tar.gz"
      sha256 "d111bf753f958189cd20296eda40ec78e77c3f5894547c3e171cba4cc70cb9fc"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
