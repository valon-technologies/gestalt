# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.27"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.27/gestaltd-macos-arm64.tar.gz"
      sha256 "5185b61a298a2e8b3abbc45dcdec22fcbfc1e65d3bc631dc1fa5038707c261f4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.27/gestaltd-macos-x86_64.tar.gz"
      sha256 "a4c30f13a3a93036de1d9c30a6c312a798c094edac66e1f38200708e7718c2f3"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.27/gestaltd-linux-arm64.tar.gz"
      sha256 "1cf0b590163a3a764269b796cbc57c9e5eeb15aa4d6ad785c0ab6963de426400"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.27/gestaltd-linux-x86_64.tar.gz"
      sha256 "41bcee36775f52e198b03ee1918b817622b07f433dfd7a86ef16396f2f9df7aa"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
