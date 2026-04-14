# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.1/gestalt-macos-arm64.tar.gz"
      sha256 "041cb6c833d2dc2aec7ec594f36d98b35694d49b5997836dfbfb094ed4fa8973"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.1/gestalt-macos-x86_64.tar.gz"
      sha256 "1dc2d39fed7fa9c5b4964ca2913abe3a11c450baa62938a732c616a6fcacdcee"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.1/gestalt-linux-arm64.tar.gz"
      sha256 "28ed96f685fa6129c63f3b2c3476724cb6af1115f2331f2f823e6baef9e14198"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.1/gestalt-linux-x86_64.tar.gz"
      sha256 "c5d7607cb8422a67681da631709880eb804218687462181c53330579d1747ab4"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
