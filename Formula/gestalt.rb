# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.11"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.11/gestalt-macos-arm64.tar.gz"
      sha256 "17fd6b651a60506096fb5b21ece285d591cb9e4db6468708da2f1d661e8ba4ae"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.11/gestalt-macos-x86_64.tar.gz"
      sha256 "cde4eee1a0a8384ac5734d85d3dfec4286e143e0ab13ccbe7835d776f3facd0b"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.11/gestalt-linux-arm64.tar.gz"
      sha256 "c32653cfdb6ec2c00c82365f48e02c037fe2874ca7eb24bc1583110df9b746dc"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.11/gestalt-linux-x86_64.tar.gz"
      sha256 "7bb4d7e55a465c76e713881f2c67b412028f7562c50a5dc1539a7707ee02ae17"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
