# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.40"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.40/gestaltd-macos-arm64.tar.gz"
      sha256 "48d8e2853d8137c446fcaac668edb43491b4cfeb054549fee012bd7ebb9bd815"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.40/gestaltd-macos-x86_64.tar.gz"
      sha256 "b4210119f9dc455b3a3f15145c181686fceec838158972fd9e6312d07e4b2824"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.40/gestaltd-linux-arm64.tar.gz"
      sha256 "e9c9c28c1ed2086d67148c8a42f321b6ff5429b991f301a9f6dcffea0f824ced"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.40/gestaltd-linux-x86_64.tar.gz"
      sha256 "341142924f983a14c60f43c61932bc50bcb7b3b2281d39955fdead6f3c1a1acd"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
