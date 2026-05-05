# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.15"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.15/gestalt-macos-arm64.tar.gz"
      sha256 "a3b5b853994d333da255181447445a7978f51c1704801e2f0776727d74e60fb7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.15/gestalt-macos-x86_64.tar.gz"
      sha256 "5e38164316887da08aeae4a4c23b146fe11b8cbfcf1c417360961469553b7029"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.15/gestalt-linux-arm64.tar.gz"
      sha256 "3f4d3b43fce84be60a9611fc91aa01d28269e8cf2b1af7cafe8f7dcbfa542cc6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.15/gestalt-linux-x86_64.tar.gz"
      sha256 "d714c82c5cd9d8a83cbbd8bb736aef58dabe3ac5e183908c3a782a6d83d31cb1"
    end
  end

  def install
    bin.install "gestalt", "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
