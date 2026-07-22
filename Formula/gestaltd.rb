# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.46"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.46/gestaltd-macos-arm64.tar.gz"
      sha256 "76b8280a83d4bbdc5e4e595b2e0872d3d7172b3d802ba704857baa5425c4f59e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.46/gestaltd-macos-x86_64.tar.gz"
      sha256 "97c0971edb97da43eafa650d681d0c612342474a4ff11003655d00acf10e7303"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.46/gestaltd-linux-arm64.tar.gz"
      sha256 "f52107fca1ed55ceff3e8225704172437128b216fc284788f62f12aed2399285"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.46/gestaltd-linux-x86_64.tar.gz"
      sha256 "4a76c93c2f62bf85795bcf76fc65599b7121355d2437f6f6f4babe26677a837e"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
