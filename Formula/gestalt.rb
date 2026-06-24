# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.6"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.6/gestalt-macos-arm64.tar.gz"
      sha256 "ab0ddff237a83ad6d4ed3027789b35a9cecfecd70ef35056b5b697c634f9ce11"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.6/gestalt-macos-x86_64.tar.gz"
      sha256 "bcab1f6db427d09ebfed73025c441ee9d0ba4b9804db4c7f399acd007c5b15b7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.6/gestalt-linux-arm64.tar.gz"
      sha256 "3de055875e092a0302377ef5d0d977cd424af6a17b13e49a892e1ecf5cc7e206"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.6/gestalt-linux-x86_64.tar.gz"
      sha256 "229e42ea074dfdbc87e02fab33a9a8b4b6413be0e0c7004c02d53a21609e3ac0"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
