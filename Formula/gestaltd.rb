# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-macos-arm64.tar.gz"
      sha256 "ac96c711f08ea5829f57bcfbf2a76c6b1f08428fe4483e5ac575c651776c75d8"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-macos-x86_64.tar.gz"
      sha256 "7dcf5f493b346da25e7c148568de9b60860bc6364295cb28e0dc7eace155a6b6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-linux-arm64.tar.gz"
      sha256 "c2a1bb8fc1291923c76390c35b9b6494c0cd8a7c74f505149125ca52979c7d20"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-linux-x86_64.tar.gz"
      sha256 "be06a1d4874d8dcdde3b9d43f903ff2a74a361659008a51cd284ec46472e92e1"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
