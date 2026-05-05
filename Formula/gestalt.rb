# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.16"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.16/gestalt-macos-arm64.tar.gz"
      sha256 "8d97d589ce8a51f8c6dc9a9f3ae5f8e199f8d18293f5c47ff9f7c7d9fe920925"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.16/gestalt-macos-x86_64.tar.gz"
      sha256 "daff85fefdcce6cdc63f470df1dec169d80ce6a66fd3503b04457b24945a05c0"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.16/gestalt-linux-arm64.tar.gz"
      sha256 "262ff8882c418031d5d514357c5ede3feed250941d8032647e4eb6bc1a8d1ae7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.16/gestalt-linux-x86_64.tar.gz"
      sha256 "db6a8db9d93beb8fceaaaf6fd6fbc57aabc5950bffb4be3f275a0f88d8521953"
    end
  end

  def install
    bin.install "gestalt", "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
