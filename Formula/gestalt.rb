# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.2"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.2/gestalt-macos-arm64.tar.gz"
      sha256 "fd9f66bbe880c3483c0076c7fdc17d202a75723512d6013792b9af72985d1b4a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.2/gestalt-macos-x86_64.tar.gz"
      sha256 "56ec346eaf7a026ce40efb38cceb7bbce7417a256d7ff1ec6ccc78fb11e86e97"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.2/gestalt-linux-arm64.tar.gz"
      sha256 "d915eadbff75597c63633a9ad62439a294bf132fb93b9ecdfa23e2be13725b8f"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.2/gestalt-linux-x86_64.tar.gz"
      sha256 "088b7486a24e97af60fb47d8b5321e73702b90ddb4748bd22eee49330b678fcc"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
