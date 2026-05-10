# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.23.multitenant.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.23.multitenant.1/gestaltd-macos-arm64.tar.gz"
      sha256 "d6cde4bfc246712df7c42fd292ba94157a310f3a3ce1f87b65eb69e334ee6ac7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.23.multitenant.1/gestaltd-macos-x86_64.tar.gz"
      sha256 "f1e821fb20bad7ff691abd3711c98a2d9641e8958771a9e2f07df263f614f57f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.23.multitenant.1/gestaltd-linux-arm64.tar.gz"
      sha256 "132e12f38ef5dce774269e397cf6d5ebd4e2c650d6afccd0f76583247e7e33de"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.23.multitenant.1/gestaltd-linux-x86_64.tar.gz"
      sha256 "6a552ddca49bf57a0ba84c5c866d5f6040ba7f4fd97de9af114cee0b604c1a7c"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
