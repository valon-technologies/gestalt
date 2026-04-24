# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.6"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.6/gestalt-macos-arm64.tar.gz"
      sha256 "3422b27d866b3eba5b1e3ba5a09d51e60658e9653d49366bba6cc15b24f4decc"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.6/gestalt-macos-x86_64.tar.gz"
      sha256 "a956df248f52553e5270c47387e6f4a6ee707d6db3dc0da12af32ec92dcc3056"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.6/gestalt-linux-arm64.tar.gz"
      sha256 "51d258b86224ae67872f34ba02a6051ed4a2e7a8bf0fce576863159c207ff31d"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.6/gestalt-linux-x86_64.tar.gz"
      sha256 "1f0d607e25a2074078d172833ac78889a5254471674a9a24b4468f25ef350661"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
